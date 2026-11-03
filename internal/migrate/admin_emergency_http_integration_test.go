//go:build integration

package migrate

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	strategyv1 "github.com/hmlax/muskdeer/deer/proto/rpc/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/testopenapi"
)

func TestAdminEmergencyHTTPAcceptsAndObservesRealScopedCancellationWithoutConflatingReplay(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	_, _, submit := adminCreationFixture(ctx, t, db)
	riskActorFixture(ctx, t, db, "emergency-http-viewer", domain.AdminRoleViewer, 221, domain.AdminSessionTTL)
	core, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	order, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	ca, discovery := emergencyProcessDiscovery(ctx, t)
	management := businessProcessConfig(t, ca, postgres, nil, discovery.Config, "strategy")
	management.Business.Strategy = &configv1.StrategyRuntime{
		ManagementEnabled: true,
		ControlEnabled:    true,
	}
	startBusinessProcess(ctx, t, management, discovery.Client)
	base := startStrategyManagementAPI(ctx, t, ca, postgres, discovery.Config, true)
	client, jar := nativeRiskHTTPClient(t)
	csrf := nativeControlHTTPLogin(ctx, t, client, jar, base, "control-operator", domain.AdminRoleOperator)
	download := nativeAdminRequest(ctx, t, client, base, "GET", "/openapi.json", "", "", "")
	contract, err := testopenapi.New(nativeOpenAPIBody(t, download, 512*1024))
	if err != nil || download.StatusCode != 200 {
		t.Fatal("actual emergency API did not provide its generated contract", err)
	}
	request := emergencyAcceptanceRequest(before.Id)
	request.Key = "native-http-emergency-request-0001"
	accepted := nativeEmergencyPOST(ctx, t, client, base, csrf, request, 202, contract)
	if again := nativeEmergencyPOST(ctx, t, client, base, csrf, request, 202, contract); !proto.Equal(accepted, again) {
		t.Fatal("actual emergency replay changed immutable acceptance")
	}
	changed := request
	changed.Reason = "changed original emergency confirmation"
	nativeEmergencyPOST(ctx, t, client, base, csrf, changed, 409, contract)
	changed = request
	changed.Key = "native-http-emergency-stale-0001"
	nativeEmergencyPOST(ctx, t, client, base, csrf, changed, 409, contract)
	viewer, viewerJar := nativeRiskHTTPClient(t)
	viewerCSRF := nativeControlHTTPLogin(ctx, t, viewer, viewerJar, base, "emergency-http-viewer", domain.AdminRoleViewer)
	nativeEmergencyPOST(ctx, t, viewer, base, viewerCSRF, request, 403, contract)
	nativeEmergencyPOST(ctx, t, client, base, viewerCSRF, request, 403, contract)
	pending := nativeEmergencyGET(ctx, t, viewer, base, accepted, contract)
	if pending.Record.State != string(domain.AdminEmergencyCancelAccepted) || pending.Record.EnrollmentComplete || pending.Record.CompletedAtMs != nil || pending.Progress.Targets != 0 ||
		!proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) {
		t.Fatal("HTTP read advanced metadata or claimed execution before a worker existed")
	}
	nativeOpenAPILogout(ctx, t, client, jar, base, contract)
	nativeEmergencyPOST(ctx, t, client, base, csrf, request, 401, contract)
	startNativeCancellationExecution(ctx, t, db, ca, postgres, discovery.Config, discovery.Client, submit)
	waitNativeCancellationPending(ctx, t, db, order.ID)
	pending = nativeEmergencyGET(ctx, t, viewer, base, accepted, contract)
	if pending.Record.State != string(domain.AdminEmergencyCancelAccepted) || pending.Progress.Targets != 1 || pending.Progress.Terminal != 0 || pending.Record.CompletedAtMs != nil {
		t.Fatal("HTTP considered an unconfirmed core cancellation complete")
	}
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "799.8", "200.2")
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	if _, err := core.Fill(ctx, domain.CoreFillRequest{
		AccountID:           submit.Input.AccountID,
		ClientOrderID:       order.ClientOrderID,
		TradeID:             "http-emergency-in-flight-fill",
		CounterpartySubject: "external-liquidity",
		Price:               decimal.NewFromInt(100),
		Quantity:            decimal.NewFromInt(1),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.ConfirmCancel(ctx, submit.Input.AccountID, order.ClientOrderID); err != nil {
		t.Fatal(err)
	}
	done := waitNativeEmergencyComplete(ctx, t, viewer, base, accepted, contract)
	if done.Progress.Targets != 1 || done.Progress.Terminal != 1 || done.Progress.Failed != 0 || !done.Record.EnrollmentComplete || done.Record.CompletedAtMs == nil {
		t.Fatal("HTTP lost persisted scoped completion proof")
	}
	csrf = nativeControlHTTPLogin(ctx, t, client, jar, base, "control-operator", domain.AdminRoleOperator)
	if replay := nativeEmergencyPOST(ctx, t, client, base, csrf, request, 202, contract); !proto.Equal(replay, accepted) {
		t.Fatal("completed-task replay was changed into a fresh closure or terminal write response")
	}
	assertNativeEmergencyHistory(ctx, t, viewer, base, accepted, done.Record, contract)
	nativeOpenAPILogout(ctx, t, viewer, viewerJar, base, contract)
	nativeOpenAPIRead(ctx, t, viewer, base, contract, nativeEmergencyLocation(accepted), "/api/v1/strategies/{strategyID}/emergency-cancellations/{cancellationID}", 401)
	assertEmergencyProcessFinal(ctx, t, db, submit, intents, order.ID)
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "899.9", "0")
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminEmergencyCancellation{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyRiskChange{}, 1)
}

func TestAdminEmergencyHTTPQueryOnlyKeepsReadPathsAndMethodBoundary(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	request := emergencyAcceptanceRequest(submit.Input.Authority.StrategyID)
	value, err := emergencyAcceptanceStore(t, db).Request(ctx, actor.Token, actor.CSRF, request)
	if err != nil {
		t.Fatal(err)
	}
	before := emergencyQueryTask(ctx, t, db, value.ID)
	ca, discovery := emergencyProcessDiscovery(ctx, t)
	management := businessProcessConfig(t, ca, postgres, nil, discovery.Config, "strategy")
	management.Business.Strategy = &configv1.StrategyRuntime{ManagementEnabled: true}
	startBusinessProcess(ctx, t, management, discovery.Client)
	base := startStrategyManagementAPI(ctx, t, ca, postgres, discovery.Config, false)
	client, jar := nativeRiskHTTPClient(t)
	csrf := nativeCreationLogin(ctx, t, client, jar, base, "control-operator")
	sessionReply := nativeAdminRequest(ctx, t, client, base, "GET", "/api/v1/auth/session", "", "", "")
	var session struct {
		Session struct {
			Role     domain.AdminRole `json:"role"`
			Queries  bool             `json:"strategy_queries_enabled"`
			Controls bool             `json:"strategy_controls_enabled"`
		} `json:"session"`
	}
	if err := json.Unmarshal(nativeOpenAPIBody(t, sessionReply, 4096), &session); err != nil || sessionReply.StatusCode != 200 || session.Session.Role != domain.AdminRoleOperator || !session.Session.Queries || session.Session.Controls {
		t.Fatal("actual query-only session lost its explicit capability boundary", err)
	}
	download := nativeAdminRequest(ctx, t, client, base, "GET", "/openapi.json", "", "", "")
	contract, err := testopenapi.New(nativeOpenAPIBody(t, download, 512*1024))
	if err != nil || download.StatusCode != 200 {
		t.Fatal("query-only API did not expose its authenticated contract", err)
	}
	nativeEmergencyPOST(ctx, t, client, base, csrf, request, 405, contract)
	accepted := &strategyv1.EmergencyCancellationAcceptance{
		Id:                   value.ID,
		ActorUserId:          value.ActorUserID,
		StrategyId:           value.StrategyID,
		Scope:                string(value.Scope),
		ExpectedRiskRevision: value.ExpectedRiskRevision,
		RiskRevision:         value.RiskRevision,
		Reason:               value.Reason,
		CreatedAtMs:          value.CreatedAt.UnixMilli(),
	}
	observed := nativeEmergencyGET(ctx, t, client, base, accepted, contract)
	if observed.Record.State != string(domain.AdminEmergencyCancelAccepted) || observed.Record.EnrollmentComplete || observed.Record.CompletedAtMs != nil ||
		!proto.Equal(before, emergencyQueryTask(ctx, t, db, accepted.Id)) {
		t.Fatal("query-only HTTP advanced durable empty work")
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyAction{}, 0)
	assertGatewayOrderCount(ctx, t, db, 0)
}

func nativeEmergencyPOST(ctx context.Context, t *testing.T, client *http.Client, base, csrf string, request domain.AdminEmergencyCancelRequest, expected int, contract *testopenapi.Contract) *strategyv1.EmergencyCancellationAcceptance {
	t.Helper()
	data, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(&strategyv1.EmergencyCancellationInput{
		Scope:                string(request.Scope),
		ExpectedRiskRevision: request.ExpectedRiskRevision,
		Reason:               request.Reason,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/strategies/" + strconv.FormatInt(request.StrategyID, 10) + "/emergency-cancellations"
	response := nativeAdminRequest(ctx, t, client, base, "POST", path, string(data), base, csrf, nativeAdminHeader{
		Key:   "Idempotency-Key",
		Value: request.Key,
	})
	body := nativeOpenAPIBody(t, response, 8192)
	if response.StatusCode != expected {
		t.Fatalf("emergency HTTP status=%d want=%d", response.StatusCode, expected)
	}
	if err := contract.ValidateResponse("POST", "/api/v1/strategies/{strategyID}/emergency-cancellations", expected, body); err != nil {
		t.Fatal(err)
	}
	if expected >= 400 {
		return nil
	}
	var accepted strategyv1.EmergencyCancellationAcceptance
	if err := protojson.Unmarshal(body, &accepted); err != nil || response.Header.Get("Location") != nativeEmergencyLocation(&accepted) || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("native emergency acceptance lost typed identity/location/cache boundary", err)
	}
	return &accepted
}

func nativeEmergencyLocation(accepted *strategyv1.EmergencyCancellationAcceptance) string {
	return "/api/v1/strategies/" + strconv.FormatInt(accepted.StrategyId, 10) + "/emergency-cancellations/" + strconv.FormatInt(accepted.Id, 10)
}

func nativeEmergencyGET(ctx context.Context, t *testing.T, client *http.Client, base string, accepted *strategyv1.EmergencyCancellationAcceptance, contract *testopenapi.Contract) *strategyv1.EmergencyCancellationObservation {
	t.Helper()
	body := nativeOpenAPIRead(ctx, t, client, base, contract, nativeEmergencyLocation(accepted), "/api/v1/strategies/{strategyID}/emergency-cancellations/{cancellationID}", 200)
	var observed strategyv1.EmergencyCancellationObservation
	if err := protojson.Unmarshal(body, &observed); err != nil || !proto.Equal(observed.GetRecord().GetAcceptance(), accepted) {
		t.Fatal("native emergency observation lost its immutable acceptance", err)
	}
	return &observed
}

func waitNativeEmergencyComplete(ctx context.Context, t *testing.T, client *http.Client, base string, accepted *strategyv1.EmergencyCancellationAcceptance, contract *testopenapi.Contract) *strategyv1.EmergencyCancellationObservation {
	t.Helper()
	call, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		observed := nativeEmergencyGET(call, t, client, base, accepted, contract)
		if observed.Record.State == string(domain.AdminEmergencyCancelSucceeded) {
			return observed
		}
		select {
		case <-ticker.C:
		case <-call.Done():
			t.Fatal("actual scoped cancellation did not persist its terminal proof", call.Err())
		}
	}
}

func assertNativeEmergencyHistory(ctx context.Context, t *testing.T, client *http.Client, base string, accepted *strategyv1.EmergencyCancellationAcceptance, done *strategyv1.EmergencyCancellationRecord, contract *testopenapi.Contract) {
	t.Helper()
	path := "/api/v1/strategies/" + strconv.FormatInt(accepted.StrategyId, 10) + "/emergency-cancellations"
	body := nativeOpenAPIRead(ctx, t, client, base, contract, path+"?limit=1", "/api/v1/strategies/{strategyID}/emergency-cancellations", 200)
	var history strategyv1.ListEmergencyCancellationsReply
	if err := protojson.Unmarshal(body, &history); err != nil || len(history.Items) != 1 || !proto.Equal(history.Items[0], done) || history.NextAfterId != 0 {
		t.Fatal("native history lost its stored task result", err)
	}
	body = nativeOpenAPIRead(ctx, t, client, base, contract, path+"?after_id="+strconv.FormatInt(accepted.Id, 10), "/api/v1/strategies/{strategyID}/emergency-cancellations", 200)
	if err := protojson.Unmarshal(body, &history); err != nil || len(history.Items) != 0 || history.NextAfterId != 0 {
		t.Fatal("exhausted native emergency history repeated its scope", err)
	}
}
