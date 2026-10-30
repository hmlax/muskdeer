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

	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"gorm.io/gorm"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestAdminCancellationHTTPRunsThroughRealOwnedExecutionAndKeepsFundedProof(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	riskActorFixture(ctx, t, db, "cancellation-viewer", domain.AdminRoleViewer, 61, domain.AdminSessionTTL)
	core, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	order, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	// Keep risk closed throughout. Controlled cancellation must work without
	// enabling quotes or depending on a running market service.
	if _, err := riskControlFixture(t, db).Change(ctx, actor.Token, actor.CSRF, riskBlockRequest(submit.Input.Authority.StrategyID)); err != nil {
		t.Fatal(err)
	}
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	ca, discovery, naming := cancellationDiscovery(ctx, t)
	management := businessProcessConfig(t, ca, postgres, nil, naming, "strategy")
	management.Business.Strategy = &configv1.StrategyRuntime{
		ManagementEnabled: true,
		ControlEnabled:    true,
	}
	startBusinessProcess(ctx, t, management, discovery)
	base := startStrategyManagementAPI(ctx, t, ca, postgres, naming, true)
	client, jar := nativeRiskHTTPClient(t)
	csrf := nativeControlHTTPLogin(ctx, t, client, jar, base, "control-operator", domain.AdminRoleOperator)
	request := adminCancelRequest(order)
	first := nativeCancellationPOST(ctx, t, client, base, csrf, request, 202)
	if again := nativeCancellationPOST(ctx, t, client, base, csrf, request, 202); !proto.Equal(first, again) || first.Observation != nil || first.CompletedAtMs != nil {
		t.Fatal("actual HTTP replay duplicated acceptance or fabricated completion")
	}
	assertNativeCancellationRejectedInputs(ctx, t, client, base, csrf, request)
	assertNativeCancellationPeer(ctx, t, ca, discovery, actor.Token, first, true)
	viewer, viewerJar := nativeRiskHTTPClient(t)
	viewerCSRF := nativeControlHTTPLogin(ctx, t, viewer, viewerJar, base, "cancellation-viewer", domain.AdminRoleViewer)
	nativeCancellationPOST(ctx, t, viewer, base, viewerCSRF, request, 403)
	var accepted strategyv1.OrderCancellationReceipt
	readNativeStrategyProto(ctx, t, viewer, base, nativeCancellationLocation(first), &accepted)
	if !proto.Equal(first, &accepted) || !proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) {
		t.Fatal("management delivery changed strategy or receipt before an executor existed")
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "799.8", "200.2")
	assertPortfolioRowCount(ctx, t, db, &modelv1.CancellationRequest{}, 0)
	startNativeCancellationExecution(ctx, t, db, ca, postgres, naming, discovery, submit)
	waitNativeCancellationPending(ctx, t, db, order.ID)
	readNativeStrategyProto(ctx, t, viewer, base, nativeCancellationLocation(first), &accepted)
	if accepted.State != string(domain.AdminCancellationAccepted) || accepted.Observation != nil {
		t.Fatal("actual pending cancellation was called complete")
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	if _, err := core.Fill(ctx, domain.CoreFillRequest{
		AccountID:           submit.Input.AccountID,
		ClientOrderID:       order.ClientOrderID,
		TradeID:             "native-http-cancel-racing-fill",
		CounterpartySubject: "external-liquidity",
		Price:               decimal.NewFromInt(100),
		Quantity:            decimal.NewFromInt(1),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.ConfirmCancel(ctx, submit.Input.AccountID, order.ClientOrderID); err != nil {
		t.Fatal(err)
	}
	done := waitNativeCancellationComplete(ctx, t, viewer, base, first)
	if again := nativeCancellationPOST(ctx, t, client, base, csrf, request, 200); !proto.Equal(done, again) {
		t.Fatal("completed HTTP replay changed its original order confirmation")
	}
	assertNativeCancellationFinal(ctx, t, db, core, submit, request, done)
	assertNativeCancellationLogout(ctx, t, viewer, viewerJar, base, viewerCSRF, request, done)
}

func cancellationDiscovery(ctx context.Context, t *testing.T) (*testcert.Authority, *nacos.Client, *configv1.Nacos) {
	t.Helper()
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
	return ca, discovery, naming
}

type nativeCancellationBody struct {
	ExpectedOrderRevision string `json:"expected_order_revision"`
	Reason                string `json:"reason"`
}

func nativeCancellationPOST(ctx context.Context, t *testing.T, client *http.Client, base, csrf string, request domain.AdminCancellationRequest, expected int) *strategyv1.OrderCancellationReceipt {
	t.Helper()
	data, err := json.Marshal(nativeCancellationBody{
		ExpectedOrderRevision: strconv.FormatInt(request.ExpectedOrderRevision, 10),
		Reason:                request.Reason,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/strategies/" + strconv.FormatInt(request.StrategyID, 10) + "/orders/" + strconv.FormatInt(request.OrderID, 10) + "/cancellations"
	reply := nativeAdminRequest(ctx, t, client, base, "POST", path, string(data), base, csrf, nativeAdminHeader{
		Key:   "Idempotency-Key",
		Value: request.Key,
	})
	body, readErr := io.ReadAll(io.LimitReader(reply.Body, 4096))
	closeErr := reply.Body.Close()
	if readErr != nil || closeErr != nil || reply.StatusCode != expected {
		t.Fatalf("native cancellation status=%d want=%d read=%v close=%v", reply.StatusCode, expected, readErr, closeErr)
	}
	if expected >= 400 {
		return nil
	}
	var receipt strategyv1.OrderCancellationReceipt
	if err := protojson.Unmarshal(body, &receipt); err != nil || reply.Header.Get("Location") != nativeCancellationLocation(&receipt) || reply.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("native cancellation lost typed result, location or cache boundary", err)
	}
	return &receipt
}

func nativeCancellationLocation(receipt *strategyv1.OrderCancellationReceipt) string {
	return "/api/v1/strategies/" + strconv.FormatInt(receipt.StrategyId, 10) + "/order-cancellations/" + strconv.FormatInt(receipt.Id, 10)
}

func assertNativeCancellationRejectedInputs(ctx context.Context, t *testing.T, client *http.Client, base, csrf string, original domain.AdminCancellationRequest) {
	t.Helper()
	changed := original
	changed.Reason = "different content on the original key"
	nativeCancellationPOST(ctx, t, client, base, csrf, changed, 409)
	changed = original
	changed.Key, changed.ExpectedOrderRevision = "native-cancel-stale-revision", original.ExpectedOrderRevision+1
	nativeCancellationPOST(ctx, t, client, base, csrf, changed, 409)
	changed = original
	changed.Key, changed.StrategyID = "native-cancel-wrong-scope", original.StrategyID+1
	nativeCancellationPOST(ctx, t, client, base, csrf, changed, 404)
}

func assertNativeCancellationPeer(ctx context.Context, t *testing.T, ca *testcert.Authority, discovery *nacos.Client, token string, receipt *strategyv1.OrderCancellationReceipt, controls bool) {
	t.Helper()
	api := adminStrategyProcessConnection(ctx, t, ca, discovery, rpcsecurity.ServiceAPI)
	request := &strategyv1.GetOrderCancellationRequest{
		Session:        &strategyv1.AdminSession{Token: token},
		StrategyId:     receipt.StrategyId,
		CancellationId: receipt.Id,
	}
	if value, err := strategyv1.NewStrategyOrderCancellationQueryClient(api).GetOrderCancellation(ctx, request); err != nil || !proto.Equal(value, receipt) {
		t.Fatal("real API-peer cancellation read failed", err)
	}
	other := adminStrategyProcessConnection(ctx, t, ca, discovery, rpcsecurity.ServicePortfolio)
	if _, err := strategyv1.NewStrategyOrderCancellationQueryClient(other).GetOrderCancellation(ctx, request); status.Code(err) != codes.PermissionDenied {
		t.Fatal("valid other-service certificate read cancellation history", err)
	}
	if _, err := strategyv1.NewStrategyOrderCancellationControlClient(other).RequestOrderCancellation(ctx, &strategyv1.RequestOrderCancellationRequest{}); status.Code(err) != codes.PermissionDenied && controls {
		t.Fatal("valid other-service certificate requested cancellation", err)
	}
	if !controls {
		if _, err := strategyv1.NewStrategyOrderCancellationControlClient(api).RequestOrderCancellation(ctx, &strategyv1.RequestOrderCancellationRequest{}); status.Code(err) != codes.Unimplemented {
			t.Fatal("query-only strategy registered cancellation writes", err)
		}
	}
}

func startNativeCancellationExecution(ctx context.Context, t *testing.T, db *gorm.DB, ca *testcert.Authority, postgres *configv1.Postgres, naming *configv1.Nacos, discovery *nacos.Client, submit domain.GatewaySubmit) {
	t.Helper()
	expireLeaseFixture(ctx, t, db, submit.ExecutionAuthority.Key)
	expireLeaseFixture(ctx, t, db, domain.LeaseKey{
		Kind:       domain.LeaseKindStrategy,
		StrategyID: submit.Input.Authority.StrategyID,
	})
	execution := businessProcessConfig(t, ca, postgres, businessStream(ctx, t), naming, "execution")
	execution.Business.OperationTimeout = durationpb.New(time.Second)
	execution.Business.ExecutionLeaseTtl = durationpb.New(3 * time.Second)
	execution.Business.Risk = &configv1.RiskRuntime{AccountIds: []string{submit.Input.AccountID}}
	startBusinessProcess(ctx, t, execution, discovery)
	strategy := strategyProcessConfig(t, ca, postgres, naming, submit.Input.AccountID, "native-order-cancellation")
	strategy.Business.Strategy.ManagementEnabled, strategy.Business.Strategy.ControlEnabled = true, true
	startBusinessProcess(ctx, t, strategy, discovery)
}

func waitNativeCancellationPending(ctx context.Context, t *testing.T, db *gorm.DB, orderID int64) {
	t.Helper()
	call, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var count int64
		if err := db.WithContext(call).Model(&modelv1.CancellationRequest{}).Where("intent_id = ? AND state = ?", orderID, domain.CancellationPending).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count == 1 {
			return
		}
		select {
		case <-ticker.C:
		case <-call.Done():
			t.Fatal("actual account controller did not dispatch original management cancellation", call.Err())
		}
	}
}

func waitNativeCancellationComplete(ctx context.Context, t *testing.T, client *http.Client, base string, original *strategyv1.OrderCancellationReceipt) *strategyv1.OrderCancellationReceipt {
	t.Helper()
	call, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var receipt strategyv1.OrderCancellationReceipt
		readNativeStrategyProto(call, t, client, base, nativeCancellationLocation(original), &receipt)
		if receipt.State == string(domain.AdminCancellationSucceeded) {
			return &receipt
		}
		select {
		case <-ticker.C:
		case <-call.Done():
			t.Fatal("actual cancellation receipt did not reach terminal proof", call.Err())
		}
	}
}

func assertNativeCancellationFinal(ctx context.Context, t *testing.T, db *gorm.DB, core domain.Simulator, submit domain.GatewaySubmit, request domain.AdminCancellationRequest, done *strategyv1.OrderCancellationReceipt) {
	t.Helper()
	if done.OrderId != request.OrderID || done.ExpectedOrderRevision != request.ExpectedOrderRevision || done.Observation == nil || done.Observation.State != string(domain.OrderStateCanceled) || done.Observation.OrderRevision <= request.ExpectedOrderRevision || done.CompletedAtMs == nil {
		t.Fatal("actual cancellation lost explicit identity or terminal OMS proof")
	}
	var action modelv1.StrategyAction
	if err := db.WithContext(ctx).Model(&modelv1.StrategyAction{}).Take(&action).Error; err != nil || action.DecisionKey != domain.AdminCancellationDecisionPrefix+strconv.FormatInt(done.Id, 10) || action.GetIntentId() != request.OrderID {
		t.Fatal("actual executor did not use the durable management action", err)
	}
	instance := publicationInstance(ctx, t, db, request.StrategyID)
	if !instance.DesiredRunning || instance.DesiredState != string(domain.StrategyStateRunning) || instance.RequestedConfigVersion != 1 || instance.MarketDataSource != string(domain.MarketSourceBinance) || activationBudget(ctx, t, db, instance.Id).Enabled {
		t.Fatal("order cancellation changed strategy target, source or blocked risk flag")
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "899.9", "0")
	assertPortfolioRowCount(ctx, t, db, &modelv1.OrderIntent{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyAction{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.CancellationRequest{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminOrderCancellation{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorFill{}, 1)
	assertGatewayOrderCount(ctx, t, db, 1)
}

func assertNativeCancellationLogout(ctx context.Context, t *testing.T, client *http.Client, jar *cookiejar.Jar, base, csrf string, original domain.AdminCancellationRequest, receipt *strategyv1.OrderCancellationReceipt) {
	t.Helper()
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	cookies := jar.Cookies(parsed)
	reply := nativeAdminRequest(ctx, t, client, base, "POST", "/api/v1/auth/logout", "", base, csrf)
	if err := reply.Body.Close(); err != nil || reply.StatusCode != 204 {
		t.Fatal("actual cancellation viewer logout failed", err)
	}
	jar.SetCookies(parsed, cookies)
	nativeCancellationPOST(ctx, t, client, base, csrf, original, 401)
	jar.SetCookies(parsed, cookies)
	reply = nativeAdminRequest(ctx, t, client, base, "GET", nativeCancellationLocation(receipt), "", "", "")
	if err := reply.Body.Close(); err != nil || reply.StatusCode != 401 {
		t.Fatal("restored revoked session read cancellation history", err)
	}
}

func TestAdminCancellationQueryOnlyProcessDoesNotRegisterWrites(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	_, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	order, err := intents.Accept(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	store, err := repository.NewAdminOrderCancellations(db)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := store.Request(ctx, actor.Token, actor.CSRF, adminCancelRequest(order))
	if err != nil {
		t.Fatal(err)
	}
	ca, discovery, naming := cancellationDiscovery(ctx, t)
	strategy := businessProcessConfig(t, ca, postgres, nil, naming, "strategy")
	strategy.Business.Strategy = &configv1.StrategyRuntime{ManagementEnabled: true}
	startBusinessProcess(ctx, t, strategy, discovery)
	assertNativeCancellationPeer(ctx, t, ca, discovery, actor.Token, &strategyv1.OrderCancellationReceipt{
		Id:                    receipt.ID,
		ActorUserId:           receipt.ActorUserID,
		StrategyId:            receipt.StrategyID,
		OrderId:               receipt.OrderID,
		ExpectedOrderRevision: receipt.ExpectedOrderRevision,
		Reason:                receipt.Reason,
		State:                 string(receipt.State),
		OutcomeCode:           receipt.Outcome,
		Revision:              receipt.Revision,
		CreatedAtMs:           receipt.CreatedAt.UnixMilli(),
	}, false)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyAction{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.CancellationRequest{}, 0)
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
}
