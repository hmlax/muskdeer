//go:build integration

package migrate

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategyconfig"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestAdminPublicationHTTPUsesRealAuthorityAndTypedVersionTransaction(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	_, _, submit := adminCreationFixture(ctx, t, db)
	if err := db.WithContext(ctx).Model(&modelv1.AdminUser{}).Create(&modelv1.AdminUser{
		Username:          "publication-viewer",
		PasswordHash:      controlFixturePasswordHash,
		Role:              string(domain.AdminRoleViewer),
		Enabled:           true,
		CredentialVersion: 1,
		Revision:          1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	var original []*modelv1.SimulatorBalance
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorBalance{}).Order("id ASC").Find(&original).Error; err != nil {
		t.Fatal(err)
	}
	ca := testcert.New(t)
	naming := businessNaming()
	discovery, err := nacos.New(ctx, naming)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := discovery.Close(); err != nil {
			t.Error(err)
		}
	})
	strategy := businessProcessConfig(t, ca, postgres, nil, naming, "strategy")
	strategy.Business.Strategy = &configv1.StrategyRuntime{
		ManagementEnabled: true,
		ControlEnabled:    true,
	}
	startBusinessProcess(ctx, t, strategy, discovery)
	base := startStrategyManagementAPI(ctx, t, ca, postgres, naming, true)
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
	csrf := nativeCreationLogin(ctx, t, client, jar, base, "control-operator")
	var last domain.AdminPublishStrategy
	for _, template := range []domain.StrategyTemplate{domain.StrategyTemplateMarketMaker, domain.StrategyTemplateGrid} {
		for _, source := range []domain.MarketSource{domain.MarketSourceBinance, domain.MarketSourceOKX} {
			creation := adminCreationRequest(t, submit, template, source)
			created := nativeCreationPOST(ctx, t, client, base, csrf, creation, http.StatusCreated)
			before := publicationInstance(ctx, t, db, created.StrategyId)
			budget := activationBudget(ctx, t, db, before.Id)
			last = publicationRequest(creation, before.Id, before.Revision, before.RequestedConfigVersion)
			receipt := nativePublicationPOST(ctx, t, client, base, csrf, last, http.StatusCreated)
			again := nativePublicationPOST(ctx, t, client, base, csrf, last, http.StatusCreated)
			if !proto.Equal(receipt, again) || receipt.ActorUserId <= 0 || receipt.ConfigVersion != 2 || receipt.PreviousConfigVersion != 1 || receipt.PreviousRevision != 1 || receipt.Revision != 2 || receipt.MarketDataSource != string(last.Source) {
				t.Fatal("actual publication replay lost version/source identity")
			}
			assertPublicationEffect(ctx, t, db, before, budget, last, domain.AdminPublicationReceipt{
				ConfigVersion: receipt.ConfigVersion,
				Revision:      receipt.Revision,
			})
		}
	}
	changed := last
	changed.Reason += " changed"
	nativePublicationPOST(ctx, t, client, base, csrf, changed, http.StatusConflict)
	changed = last
	changed.Key += "-stale"
	nativePublicationPOST(ctx, t, client, base, csrf, changed, http.StatusConflict)
	peer := strategyv1.NewStrategyPublicationClient(adminStrategyProcessConnection(ctx, t, ca, discovery, rpcsecurity.ServicePortfolio))
	call, cancelCall := context.WithTimeout(ctx, time.Second)
	_, peerErr := peer.PublishConfiguration(call, &strategyv1.PublishConfigurationRequest{})
	cancelCall()
	if status.Code(peerErr) != codes.PermissionDenied {
		t.Fatal("non-API mTLS peer published a configuration", peerErr)
	}
	queryBase := startStrategyManagementAPI(ctx, t, ca, postgres, naming, false)
	queryReply := nativeAdminRequest(ctx, t, client, queryBase, "POST", "/api/v1/strategies/1/configurations", `{}`, queryBase, csrf)
	if err := queryReply.Body.Close(); err != nil || queryReply.StatusCode != http.StatusNotFound {
		t.Fatal("query-only API exposed configuration publication")
	}
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	cookies := jar.Cookies(parsed)
	logout := nativeAdminRequest(ctx, t, client, base, "POST", "/api/v1/auth/logout", "", base, csrf)
	if err := logout.Body.Close(); err != nil || logout.StatusCode != http.StatusNoContent {
		t.Fatal("publication operator logout failed")
	}
	jar.SetCookies(parsed, cookies)
	nativePublicationPOST(ctx, t, client, base, csrf, last, http.StatusUnauthorized)
	csrf = nativeCreationLogin(ctx, t, client, jar, base, "publication-viewer")
	nativePublicationPOST(ctx, t, client, base, csrf, last, http.StatusForbidden)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyPublication{}, 4)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyConfigVersion{}, 9)
	assertGatewayOrderCount(ctx, t, db, 0)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	var balances []*modelv1.SimulatorBalance
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorBalance{}).Order("id ASC").Find(&balances).Error; err != nil || len(balances) != len(original) {
		t.Fatal("publication changed core balance rows", err)
	}
	for index := range balances {
		if !proto.Equal(original[index], balances[index]) {
			t.Fatal("publication changed a core ledger balance")
		}
	}
}

func nativePublicationPOST(ctx context.Context, t *testing.T, client *http.Client, base, csrf string, request domain.AdminPublishStrategy, expected int) *strategyv1.PublicationReceipt {
	t.Helper()
	parameters, err := strategyconfig.Encode(request.Parameters)
	if err != nil {
		t.Fatal(err)
	}
	body, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(&strategyv1.PublishConfigurationInput{
		ExpectedRevision:      request.ExpectedRevision,
		ExpectedConfigVersion: request.ExpectedConfigVersion,
		MarketDataSource:      string(request.Source),
		Parameters:            parameters,
		Reason:                request.Reason,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/strategies/" + strconv.FormatInt(request.StrategyID, 10) + "/configurations"
	reply := nativeAdminRequest(ctx, t, client, base, "POST", path, string(body), base, csrf, nativeAdminHeader{
		Key:   "Idempotency-Key",
		Value: request.Key,
	})
	data, err := io.ReadAll(io.LimitReader(reply.Body, 16384))
	closeErr := reply.Body.Close()
	if err != nil || closeErr != nil || reply.StatusCode != expected || reply.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("actual publication HTTP returned an unexpected status or body", reply.StatusCode, expected)
	}
	if expected != http.StatusCreated {
		return nil
	}
	var receipt strategyv1.PublicationReceipt
	if err := protojson.Unmarshal(data, &receipt); err != nil || receipt.Id <= 0 || receipt.StrategyId != request.StrategyID || reply.Header.Get("Location") != "/api/v1/strategies/"+strconv.FormatInt(request.StrategyID, 10) {
		t.Fatal("actual publication HTTP omitted its typed receipt/location", err)
	}
	return &receipt
}
