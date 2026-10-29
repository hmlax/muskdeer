//go:build integration

package migrate

import (
	"context"
	"encoding/json"
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
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestAdminRiskHTTPKeepsFundedObligationsAndRequiresFreshAdministratorRecovery(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	_, _, submit := adminCreationFixture(ctx, t, db)
	riskActorFixture(ctx, t, db, "risk-administrator", domain.AdminRoleAdmin, 61, domain.AdminSessionTTL)
	riskActorFixture(ctx, t, db, "risk-viewer", domain.AdminRoleViewer, 71, domain.AdminSessionTTL)
	core, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
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
	operator, jar := nativeRiskHTTPClient(t)
	csrf := nativeControlHTTPLogin(ctx, t, operator, jar, base, "control-operator", domain.AdminRoleOperator)
	block := riskBlockRequest(before.Id)
	first := nativeRiskPOST(ctx, t, operator, base, csrf, block, 200)
	again := nativeRiskPOST(ctx, t, operator, base, csrf, block, 200)
	if first == nil || !proto.Equal(first, again) || first.Enabled || !first.PreviousEnabled || first.Revision != 2 {
		t.Fatal("actual risk block replay lost its one immutable effect")
	}
	restore := riskRestoreRequest(before)
	nativeRiskPOST(ctx, t, operator, base, csrf, restore, 403)
	admin, adminJar := nativeRiskHTTPClient(t)
	adminCSRF := nativeControlHTTPLogin(ctx, t, admin, adminJar, base, "risk-administrator", domain.AdminRoleAdmin)
	nativeRiskPOST(ctx, t, admin, base, adminCSRF, restore, 409)
	cancels := cancellationServiceFixture(t, db, intents, core)
	pending, err := cancels.Cancel(ctx, cancelInputFixture(ctx, submit, live.ClientOrderID, "risk-http-confirmed-cancel-01"), submit.ExecutionAuthority)
	if err != nil || pending.Request.State != domain.CancellationPending {
		t.Fatal("blocked strategy lost fenced cancellation capability", err)
	}
	nativeRiskPOST(ctx, t, admin, base, adminCSRF, restore, 409)
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "799.8", "200.2")
	if _, err := core.ConfirmCancel(ctx, submit.Input.AccountID, live.ClientOrderID); err != nil {
		t.Fatal(err)
	}
	if result, err := cancels.Recover(ctx, submit.Input.AccountID, pending.Request.ID); err != nil || result.Request.State != domain.CancellationConfirmed {
		t.Fatal(err)
	}
	refreshRiskSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	if receipt := nativeRiskPOST(ctx, t, admin, base, adminCSRF, restore, 200); !receipt.Enabled || receipt.Revision != 3 || receipt.ExpectedConfigVersion != before.RequestedConfigVersion {
		t.Fatal("actual admin recovery lost independent version or confirmation")
	}
	if replay := nativeRiskPOST(ctx, t, operator, base, csrf, block, 200); !proto.Equal(replay, first) {
		t.Fatal("historical block replay changed later recovery")
	}
	assertNativeRiskState(ctx, t, admin, base, before.Id, true, 3, "0")
	viewer, viewerJar := nativeRiskHTTPClient(t)
	viewerCSRF := nativeControlHTTPLogin(ctx, t, viewer, viewerJar, base, "risk-viewer", domain.AdminRoleViewer)
	assertNativeRiskState(ctx, t, viewer, base, before.Id, true, 3, "0")
	nativeRiskPOST(ctx, t, viewer, base, viewerCSRF, block, 403)
	assertRiskLogout(ctx, t, operator, jar, base, csrf, block)
	var risk modelv1.RiskSnapshot
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ?", before.Id).Take(&risk).Error; err != nil || risk.AllowNewRisk {
		t.Fatal("HTTP risk recovery directly issued trading authority", err)
	}
	if !proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) {
		t.Fatal("risk HTTP implicitly changed runtime/configuration/ownership")
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyRiskChange{}, 2)
	assertGatewayOrderCount(ctx, t, db, 1)
}

func TestAdminRiskQueryOnlyProcessHasNoControlRPCAndRejectsOtherPeer(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
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
	strategy.Business.Strategy = &configv1.StrategyRuntime{ManagementEnabled: true}
	startBusinessProcess(ctx, t, strategy, discovery)
	api := adminStrategyProcessConnection(ctx, t, ca, discovery, rpcsecurity.ServiceAPI)
	request := &strategyv1.GetStrategyRiskRequest{
		Session:    &strategyv1.AdminSession{Token: actor.Token},
		StrategyId: submit.Input.Authority.StrategyID,
	}
	if value, err := strategyv1.NewStrategyRiskQueryClient(api).GetStrategyRisk(ctx, request); err != nil || !value.Enabled || value.Revision != 1 || value.ReservedNotional != "0" {
		t.Fatal("query-only process failed its current-authority risk read", err)
	}
	if _, err := strategyv1.NewStrategyRiskControlClient(api).ChangeStrategyRisk(ctx, &strategyv1.ChangeStrategyRiskRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatal("query-only process registered risk controls", err)
	}
	other := adminStrategyProcessConnection(ctx, t, ca, discovery, rpcsecurity.ServicePortfolio)
	if _, err := strategyv1.NewStrategyRiskQueryClient(other).GetStrategyRisk(ctx, request); status.Code(err) != codes.PermissionDenied {
		t.Fatal("valid non-API certificate read managed risk", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyRiskChange{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 0)
	assertGatewayOrderCount(ctx, t, db, 0)
}

func nativeRiskHTTPClient(t *testing.T) (*http.Client, *cookiejar.Jar) {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{
		MaxIdleConns:    2,
		MaxConnsPerHost: 2,
	}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{
		Jar:       jar,
		Transport: transport,
		Timeout:   3 * time.Second,
	}, jar
}

func nativeRiskPOST(ctx context.Context, t *testing.T, client *http.Client, base, csrf string, request domain.AdminRiskRequest, expected int) *strategyv1.StrategyRiskReceipt {
	t.Helper()
	input := &strategyv1.ChangeStrategyRiskInput{
		Action:                string(request.Action),
		ExpectedRevision:      request.ExpectedRevision,
		Reason:                request.Reason,
		ExpectedConfigVersion: request.ExpectedConfigVersion,
		ExpectedDesiredState:  string(request.ExpectedDesiredState),
	}
	body, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/strategies/" + strconv.FormatInt(request.StrategyID, 10) + "/risk"
	reply := nativeAdminRequest(ctx, t, client, base, "POST", path, string(body), base, csrf, nativeAdminHeader{
		Key:   "Idempotency-Key",
		Value: request.Key,
	})
	data, readErr := io.ReadAll(io.LimitReader(reply.Body, 4096))
	closeErr := reply.Body.Close()
	if readErr != nil || closeErr != nil || reply.StatusCode != expected || reply.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("actual risk HTTP actual=%d expected=%d read=%v close=%v", reply.StatusCode, expected, readErr, closeErr)
	}
	if expected == 409 {
		var failure struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(data, &failure); err != nil || failure.Code != domain.AdminRiskRecoveryDeniedCode || failure.Message != domain.MessageAdminRiskRecoveryDenied {
			t.Fatal("actual recovery denial lost its typed public reason", err)
		}
	}
	if expected != 200 {
		return nil
	}
	var receipt strategyv1.StrategyRiskReceipt
	if err := protojson.Unmarshal(data, &receipt); err != nil || reply.Header.Get("Location") != path {
		t.Fatal("actual risk HTTP lost typed receipt or current-state link", err)
	}
	return &receipt
}

func assertNativeRiskState(ctx context.Context, t *testing.T, client *http.Client, base string, id int64, enabled bool, revision int64, reserved string) {
	t.Helper()
	var state strategyv1.StrategyRiskState
	readNativeStrategyProto(ctx, t, client, base, "/api/v1/strategies/"+strconv.FormatInt(id, 10)+"/risk", &state)
	if state.StrategyId != id || state.Enabled != enabled || state.Revision != revision || state.ReservedNotional != reserved || state.LimitNotional != "300" {
		t.Fatal("actual risk HTTP current state lost independent flag, capacity or reservation")
	}
}

func assertRiskLogout(ctx context.Context, t *testing.T, client *http.Client, jar *cookiejar.Jar, base, csrf string, block domain.AdminRiskRequest) {
	t.Helper()
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	cookies := jar.Cookies(parsed)
	reply := nativeAdminRequest(ctx, t, client, base, "POST", "/api/v1/auth/logout", "", base, csrf)
	if err := reply.Body.Close(); err != nil || reply.StatusCode != 204 {
		t.Fatal("actual risk operator logout failed", err)
	}
	jar.SetCookies(parsed, cookies)
	nativeRiskPOST(ctx, t, client, base, csrf, block, 401)
	read := nativeAdminRequest(ctx, t, client, base, "GET", "/api/v1/strategies/"+strconv.FormatInt(block.StrategyID, 10)+"/risk", "", "", "")
	if err := read.Body.Close(); err != nil || read.StatusCode != 401 {
		t.Fatal("revoked risk operator could still read current state", err)
	}
}
