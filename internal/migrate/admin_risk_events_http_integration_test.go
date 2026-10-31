//go:build integration

package migrate

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestAdminRiskEventHTTPReadsRemovedSourceAndRetiredHistoryWithoutWrites(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	_, _, submit := adminCreationFixture(ctx, t, db)
	viewer := riskActorFixture(ctx, t, db, "risk-events-viewer", domain.AdminRoleViewer, 81, domain.AdminSessionTTL)
	evaluator, err := repository.NewRiskEvaluator(db)
	if err != nil {
		t.Fatal(err)
	}
	refreshRiskSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	if decision, err := evaluator.Evaluate(ctx, submit.Input.Authority.StrategyID); err != nil || !decision.Allowed {
		t.Fatal("native risk fixture did not reach actual ready", err)
	}
	core, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.Fill(ctx, riskEventFillRequest(submit, live.ClientOrderID)); err != nil {
		t.Fatal(err)
	}
	source := riskEventSourceRow(ctx, t, db, submit.Input.Authority.StrategyID)
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("id = ?", source.Id).Delete(&modelv1.RiskSnapshot{}).Error; err != nil {
		t.Fatal(err)
	}
	history := riskEventRows(ctx, t, db, source.StrategyInstanceId)
	if len(history) != 4 {
		t.Fatal("native fixture lacks actual risk transition history")
	}
	other := adminRiskEventUnfundedSource(ctx, t, db)
	otherHistory := riskEventRows(ctx, t, db, other.StrategyInstanceId)
	projection := adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)
	strategyBefore := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
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
	assertNativeRiskEventPeer(ctx, t, ca, discovery, viewer.Token, history[3].Id)
	base := startStrategyQueryAPI(ctx, t, ca, postgres, naming)
	client, jar := nativeRiskHTTPClient(t)
	nativeCreationLogin(ctx, t, client, jar, base, "risk-events-viewer")
	path := "/api/v1/risk-events"
	assertNativeRiskEventPages(ctx, t, client, base, path, history, otherHistory[0])
	assertNativeRiskEventErrors(ctx, t, client, base, path, otherHistory[0].Id+1)
	assertNativeOrderLogout(ctx, t, client, jar, base, path, history[3].Id)
	if !proto.Equal(projection, adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)) || !proto.Equal(strategyBefore, publicationInstance(ctx, t, db, strategyBefore.Id)) ||
		!proto.Equal(other, riskEventSourceRow(ctx, t, db, other.StrategyInstanceId)) {
		t.Fatal("native management history reads changed producer or strategy state")
	}
	for i, event := range riskEventRows(ctx, t, db, source.StrategyInstanceId) {
		if !proto.Equal(event, history[i]) {
			t.Fatal("history read mutated its immutable source")
		}
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "799.8", "100.1")
	assertPortfolioRowCount(ctx, t, db, &modelv1.RiskSnapshot{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.RiskSnapshotEvent{}, 5)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorFill{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.PortfolioFill{}, 0)
	assertGatewayOrderCount(ctx, t, db, 1)
}

func assertNativeRiskEventPeer(ctx context.Context, t *testing.T, ca *testcert.Authority, discovery *nacos.Client, token string, id int64) {
	t.Helper()
	request := &strategyv1.GetRiskEventRequest{
		Session: &strategyv1.AdminSession{Token: token},
		EventId: id,
	}
	api := adminStrategyProcessConnection(ctx, t, ca, discovery, rpcsecurity.ServiceAPI)
	reply, err := strategyv1.NewStrategyRiskEventQueryClient(api).GetRiskEvent(ctx, request)
	if err != nil || reply.Event.Id != id || !reply.Event.SnapshotDeleted || reply.Event.RecordedReconciled || !reply.Event.RecordedAllowNewRisk || reply.Event.ReasonCode != string(domain.RiskReasonReady) {
		t.Fatal("actual API peer lost removed source or raw recorded diagnostic", err)
	}
	other := adminStrategyProcessConnection(ctx, t, ca, discovery, rpcsecurity.ServicePortfolio)
	if _, err := strategyv1.NewStrategyRiskEventQueryClient(other).GetRiskEvent(ctx, request); status.Code(err) != codes.PermissionDenied {
		t.Fatal("valid non-API certificate read risk history", err)
	}
	if _, err := strategyv1.NewStrategyControlClient(api).ControlStrategy(ctx, &strategyv1.ControlStrategyRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatal("query-only node registered strategy writes", err)
	}
}

func assertNativeRiskEventPages(ctx context.Context, t *testing.T, client *http.Client, base, path string, rows []*modelv1.RiskSnapshotEvent, other *modelv1.RiskSnapshotEvent) {
	t.Helper()
	scope := "strategy_id=" + strconv.FormatInt(rows[0].StrategyInstanceId, 10)
	var first, last strategyv1.ListRiskEventsReply
	readNativeStrategyProto(ctx, t, client, base, path+"?limit=1&"+scope, &first)
	if len(first.Events) != 1 || first.Events[0].Id != rows[0].Id || first.NextAfterId != rows[0].Id || first.Events[0].Kind != string(domain.RiskSnapshotEventBaseline) {
		t.Fatal("actual risk page replaced recording identity with source revision")
	}
	readNativeStrategyProto(ctx, t, client, base, path+"?limit=1&after_id="+strconv.FormatInt(rows[2].Id, 10)+"&"+scope, &last)
	if len(last.Events) != 1 || last.Events[0].Id != rows[3].Id || last.NextAfterId != 0 || last.Events[0].Kind != string(domain.RiskSnapshotEventRemoved) {
		t.Fatal("actual risk last page lost source removal")
	}
	var filtered strategyv1.ListRiskEventsReply
	query := "?" + scope + "&account_id=" + url.QueryEscape(rows[0].AccountId) + "&instrument_id=" + strconv.FormatInt(rows[0].InstrumentId, 10) + "&market_data_source=binance&kind=REMOVED&reason_code=READY"
	readNativeStrategyProto(ctx, t, client, base, path+query, &filtered)
	if len(filtered.Events) != 1 || filtered.Events[0].Id != rows[3].Id || filtered.Events[0].RecordedReconciled || !filtered.Events[0].RecordedAllowNewRisk || filtered.Events[0].SnapshotRevision != rows[3].RiskSnapshotRevision {
		t.Fatal("actual combined filter reinterpreted historical readiness")
	}
	var detail strategyv1.GetRiskEventReply
	readNativeStrategyProto(ctx, t, client, base, path+"/"+strconv.FormatInt(other.Id, 10), &detail)
	if detail.Event.Id != other.Id || !detail.Event.SnapshotDeleted || detail.Event.MarketFeedId != nil || detail.Event.MarketSequence != 9007199254740993 {
		t.Fatal("native retired/unprovisioned history lost high sequence or optional binding")
	}
	var empty strategyv1.ListRiskEventsReply
	readNativeStrategyProto(ctx, t, client, base, path+"?account_id=missing-account", &empty)
	if len(empty.Events) != 0 || empty.NextAfterId != 0 {
		t.Fatal("unknown historical account fabricated events")
	}
}

func assertNativeRiskEventErrors(ctx context.Context, t *testing.T, client *http.Client, base, path string, missingID int64) {
	t.Helper()
	for _, test := range []struct {
		method string
		path   string
		status int
	}{
		{
			method: "GET",
			path:   path + "/" + strconv.FormatInt(missingID, 10),
			status: 404,
		},
		{
			method: "GET",
			path:   path + "?strategy_id=01",
			status: 400,
		},
		{
			method: "GET",
			path:   path + "?market_data_source=bitget",
			status: 400,
		},
		{
			method: "POST",
			path:   path,
			status: 405,
		},
	} {
		reply := nativeAdminRequest(ctx, t, client, base, test.method, test.path, "", "", "")
		if err := reply.Body.Close(); err != nil || reply.StatusCode != test.status {
			t.Fatal("native risk history lost strict read boundary", err, reply.StatusCode)
		}
	}
}
