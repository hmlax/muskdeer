//go:build integration

package migrate

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestAdminStrategyHTTPQueriesRealDiscoveredProcessAndRevokedSession(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	_, _, _, instances := adminStrategiesFixture(ctx, t, db)
	ca := testcert.New(t)
	naming := businessNaming()
	observer, err := nacos.New(ctx, naming)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := observer.Close(); err != nil {
			t.Error(err)
		}
	})
	strategy := businessProcessConfig(t, ca, postgres, nil, naming, "strategy")
	strategy.Business.Strategy = &configv1.StrategyRuntime{ManagementEnabled: true}
	startBusinessProcess(ctx, t, strategy, observer)
	base := startStrategyQueryAPI(ctx, t, ca, postgres, naming)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{
		MaxIdleConns:    2,
		MaxConnsPerHost: 2,
	}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{
		Jar:       jar,
		Transport: transport,
		Timeout:   3 * time.Second,
	}
	reply := nativeAdminRequest(ctx, t, client, base, "POST", "/api/v1/auth/login", `{"username":"strategy-viewer","password":"`+adminFixturePassword+`"}`, base, "")
	var login struct {
		Session struct {
			StrategyQueriesEnabled bool `json:"strategy_queries_enabled"`
		} `json:"session"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(reply.Body, 4096)).Decode(&login)
	closeErr := reply.Body.Close()
	if reply.StatusCode != 200 || decodeErr != nil || closeErr != nil || !login.Session.StrategyQueriesEnabled {
		t.Fatal("actual API login did not expose enabled query capability")
	}
	var page strategyv1.ListStrategiesReply
	readNativeStrategyProto(ctx, t, client, base, "/api/v1/strategies?limit=1", &page)
	if len(page.Strategies) != 1 || page.Strategies[0].Id != instances[0].Id || page.NextAfterId != instances[0].Id {
		t.Fatal("HTTP did not return actual bounded strategy identities")
	}
	for _, instance := range instances {
		var detail strategyv1.GetStrategyReply
		path := "/api/v1/strategies/" + strconv.FormatInt(instance.Id, 10)
		readNativeStrategyProto(ctx, t, client, base, path, &detail)
		if detail.GetStrategy().GetId() != instance.Id || detail.GetRequestedConfig().GetMarketDataSource() != instance.MarketDataSource || detail.ActiveConfig != nil {
			t.Fatal("HTTP invented an active config or crossed source identity")
		}
		var versions strategyv1.ListConfigVersionsReply
		readNativeStrategyProto(ctx, t, client, base, path+"/versions", &versions)
		if len(versions.Versions) != 1 || versions.Versions[0].StrategyId != instance.Id {
			t.Fatal("HTTP configuration history crossed instances")
		}
	}
	var grid strategyv1.GetStrategyReply
	readNativeStrategyProto(ctx, t, client, base, "/api/v1/strategies/"+strconv.FormatInt(instances[1].Id, 10), &grid)
	if grid.GetRequestedConfig().GetParameters().GetGrid().GetLowerPrice() != "0.123456789012345678" {
		t.Fatal("actual HTTP/gRPC/PG chain lost decimal precision")
	}
	assertNativeStrategyLogout(ctx, t, client, base, jar)
	assertGatewayOrderCount(ctx, t, db, 0)
}

func startStrategyQueryAPI(ctx context.Context, t *testing.T, ca *testcert.Authority, postgres *configv1.Postgres, naming *configv1.Nacos) string {
	t.Helper()
	clients := businessRPCConfig(ca.Issue(t, rpcsecurity.ServiceAPI, false))
	clients.AllowedServices = []string{rpcsecurity.ServiceStrategy}
	cfg := &configv1.ServiceConfig{
		Name:            "api",
		InstanceId:      "strategy-query-api",
		Version:         "test",
		StartupTimeout:  durationpb.New(5 * time.Second),
		ShutdownTimeout: durationpb.New(5 * time.Second),
		Http: &configv1.Listener{
			Address:        "127.0.0.1:0",
			RequestTimeout: durationpb.New(3 * time.Second),
		},
		Monitoring: &configv1.Listener{
			Address:        "127.0.0.1:0",
			RequestTimeout: durationpb.New(time.Second),
		},
		Postgres:    proto.CloneOf(postgres),
		Nacos:       proto.CloneOf(naming),
		GrpcClients: clients,
		AdminHttp: &configv1.AdminHTTP{
			Enabled:                true,
			PublicOrigin:           "http://127.0.0.1:0",
			AllowInsecureLoopback:  true,
			StrategyQueriesEnabled: true,
		},
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "query-api.json")
	data, err := protojson.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return startNativeAdminAPI(ctx, t, path, directory)
}

func readNativeStrategyProto(ctx context.Context, t *testing.T, client *http.Client, base, path string, destination proto.Message) {
	t.Helper()
	reply := nativeAdminRequest(ctx, t, client, base, "GET", path, "", "", "")
	data, err := io.ReadAll(io.LimitReader(reply.Body, 256*1024))
	closeErr := reply.Body.Close()
	if reply.StatusCode != 200 || err != nil || closeErr != nil || reply.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("actual strategy HTTP read failed or became cacheable")
	}
	if err := protojson.Unmarshal(data, destination); err != nil {
		t.Fatal("actual strategy HTTP body was not a typed protobuf contract")
	}
}

func assertNativeStrategyLogout(ctx context.Context, t *testing.T, client *http.Client, base string, jar *cookiejar.Jar) {
	t.Helper()
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	cookies := jar.Cookies(parsed)
	var csrf string
	for _, cookie := range cookies {
		if cookie.Name == "muskdeer_csrf" {
			csrf = cookie.Value
		}
	}
	reply := nativeAdminRequest(ctx, t, client, base, "POST", "/api/v1/auth/logout", "", base, csrf)
	if err := reply.Body.Close(); err != nil || reply.StatusCode != 204 {
		t.Fatal("actual query session logout failed")
	}
	// Restore the old bearer explicitly: backend session revocation must reject
	// it even if the browser ignores the cookie deletion response.
	jar.SetCookies(parsed, cookies)
	replay := nativeAdminRequest(ctx, t, client, base, "GET", "/api/v1/strategies", "", "", "")
	if err := replay.Body.Close(); err != nil || replay.StatusCode != 401 {
		t.Fatal("business query accepted a revoked bearer through API")
	}
}
