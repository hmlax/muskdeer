//go:build integration

package migrate

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategyconfig"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestAdminCreationHTTPUsesRealOperatorTransactionAndRejectsViewerAndLogout(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	_, _, submit := adminCreationFixture(ctx, t, db)
	if err := db.WithContext(ctx).Model(&modelv1.AdminUser{}).Create(&modelv1.AdminUser{
		Username:          "creation-viewer",
		PasswordHash:      controlFixturePasswordHash,
		Role:              string(domain.AdminRoleViewer),
		Enabled:           true,
		CredentialVersion: 1,
		Revision:          1,
	}).Error; err != nil {
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
	var last domain.AdminCreateStrategy
	for _, template := range []domain.StrategyTemplate{domain.StrategyTemplateMarketMaker, domain.StrategyTemplateGrid} {
		for _, source := range []domain.MarketSource{domain.MarketSourceBinance, domain.MarketSourceOKX} {
			last = adminCreationRequest(t, submit, template, source)
			receipt := nativeCreationPOST(ctx, t, client, base, csrf, last, 201)
			again := nativeCreationPOST(ctx, t, client, base, csrf, last, 201)
			if receipt.Id != again.Id || receipt.StrategyId != again.StrategyId || receipt.MarketDataSource != string(source) || receipt.ConfigVersion != 1 || receipt.Revision != 1 {
				t.Fatal("actual HTTP creation replay lost original identity")
			}
			assertCreatedStrategy(ctx, t, db, last, domain.AdminCreationReceipt{StrategyID: receipt.StrategyId})
		}
	}
	changed := last
	changed.Reason = "changed content under original key"
	nativeCreationPOST(ctx, t, client, base, csrf, changed, 409)
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	cookies := jar.Cookies(parsed)
	logout := nativeAdminRequest(ctx, t, client, base, "POST", "/api/v1/auth/logout", "", base, csrf)
	if err := logout.Body.Close(); err != nil || logout.StatusCode != 204 {
		t.Fatal("creation operator logout failed")
	}
	jar.SetCookies(parsed, cookies)
	nativeCreationPOST(ctx, t, client, base, csrf, last, 401)
	viewerCSRF := nativeCreationLogin(ctx, t, client, jar, base, "creation-viewer")
	nativeCreationPOST(ctx, t, client, base, viewerCSRF, last, 403)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyCreation{}, 4)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyInstance{}, 5)
	assertGatewayOrderCount(ctx, t, db, 0)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
}

func nativeCreationLogin(ctx context.Context, t *testing.T, client *http.Client, jar *cookiejar.Jar, base, username string) string {
	t.Helper()
	login := nativeAdminRequest(ctx, t, client, base, "POST", "/api/v1/auth/login", `{"username":"`+username+`","password":"`+adminFixturePassword+`"}`, base, "")
	if err := login.Body.Close(); err != nil || login.StatusCode != 200 {
		t.Fatal("actual creation account could not log in")
	}
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range jar.Cookies(parsed) {
		if cookie.Name == "muskdeer_csrf" {
			return cookie.Value
		}
	}
	t.Fatal("actual login did not issue independent CSRF proof")
	return ""
}

func nativeCreationPOST(ctx context.Context, t *testing.T, client *http.Client, base, csrf string, request domain.AdminCreateStrategy, expected int) *strategyv1.CreationReceipt {
	t.Helper()
	parameters, err := strategyconfig.Encode(request.Parameters)
	if err != nil {
		t.Fatal(err)
	}
	input := &strategyv1.CreateStrategyInput{
		InstanceKey:      request.InstanceKey,
		AccountId:        request.AccountID,
		InstrumentId:     request.InstrumentID,
		CodeVersion:      request.CodeVersion,
		MarketDataSource: string(request.Source),
		MarketMode:       string(request.Mode),
		Parameters:       parameters,
		Reason:           request.Reason,
	}
	body, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	reply := nativeAdminRequest(ctx, t, client, base, "POST", "/api/v1/strategies", string(body), base, csrf, nativeAdminHeader{
		Key:   "Idempotency-Key",
		Value: request.Key,
	})
	data, err := io.ReadAll(io.LimitReader(reply.Body, 16384))
	closeErr := reply.Body.Close()
	if err != nil || closeErr != nil || reply.StatusCode != expected || reply.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("actual creation HTTP returned an unexpected status or invalid body", reply.StatusCode, expected)
	}
	if expected != 201 {
		return nil
	}
	var receipt strategyv1.CreationReceipt
	if err := protojson.Unmarshal(data, &receipt); err != nil || receipt.Id <= 0 || receipt.StrategyId <= 0 || reply.Header.Get("Location") == "" {
		t.Fatal("actual creation HTTP omitted its typed receipt/location", err)
	}
	return &receipt
}
