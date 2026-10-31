package adminhttp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

const riskEventHTTPPath = "/api/v1/risk-events"

type riskEventHTTPClient struct {
	strategyHTTPClient
	request *strategyv1.ListRiskEventsRequest
	mutate  func(*strategyv1.ListRiskEventsReply)
}

func riskEventHTTPValue() *strategyv1.RiskDiagnosticEvent {
	return &strategyv1.RiskDiagnosticEvent{
		Id:                   9007199254740993,
		RecordedAtMs:         1,
		Kind:                 string(domain.RiskSnapshotEventChanged),
		SnapshotId:           9007199254740994,
		SnapshotRevision:     9007199254740995,
		StrategyId:           2,
		AccountId:            "risk.account",
		InstrumentId:         1,
		ConfigVersion:        9007199254740996,
		MarketDataSource:     string(domain.MarketSourceOKX),
		PublisherEpoch:       9007199254740997,
		MarketSequence:       9007199254740998,
		PublicationRevision:  9007199254740999,
		SourceVersion:        9007199254741000,
		MarketMaximumAgeMs:   5000,
		ObservedAtMs:         1000,
		ExpiresAtMs:          2000,
		RecordedAllowNewRisk: true,
		ReasonCode:           string(domain.RiskReasonReady),
	}
}

func (c *riskEventHTTPClient) ListRiskEvents(ctx context.Context, request *strategyv1.ListRiskEventsRequest, _ ...grpc.CallOption) (*strategyv1.ListRiskEventsReply, error) {
	c.record(ctx, request.Session)
	c.request = request
	reply := &strategyv1.ListRiskEventsReply{
		Events:      []*strategyv1.RiskDiagnosticEvent{riskEventHTTPValue()},
		NextAfterId: 9007199254740993,
	}
	if c.mutate != nil {
		c.mutate(reply)
	}
	return reply, c.failure
}

func (c *riskEventHTTPClient) GetRiskEvent(ctx context.Context, request *strategyv1.GetRiskEventRequest, _ ...grpc.CallOption) (*strategyv1.GetRiskEventReply, error) {
	page, err := c.ListRiskEvents(ctx, &strategyv1.ListRiskEventsRequest{Session: request.Session})
	return &strategyv1.GetRiskEventReply{Event: page.Events[0]}, err
}

func riskEventHTTPFixture(t *testing.T, client strategyv1.StrategyRiskEventQueryClient) *server.Hertz {
	t.Helper()
	routes, err := New(&authFixture{}, testOrigin, true)
	if err != nil {
		t.Fatal(err)
	}
	routes.riskEvents = client
	hertz := server.New()
	routes.Register(hertz)
	return hertz
}

func TestRiskEventHTTPKeepsHighIdentityOldFlagsAndOptionalBinding(t *testing.T) {
	client := &riskEventHTTPClient{}
	path := riskEventHTTPPath + "?limit=1&after_id=9007199254740992&strategy_id=2&account_id=risk.account&instrument_id=1&market_data_source=okx&kind=CHANGED&reason_code=READY"
	reply := perform(riskEventHTTPFixture(t, client), "GET", path, "", orderHTTPCookie())
	var body struct {
		Events []struct {
			ID         string `json:"id"`
			Sequence   string `json:"market_sequence"`
			Revision   string `json:"snapshot_revision"`
			Recorded   string `json:"recorded_at_ms"`
			Expires    string `json:"expires_at_ms"`
			Reconciled bool   `json:"recorded_reconciled"`
			Allowed    bool   `json:"recorded_allow_new_risk"`
		} `json:"events"`
		Next string `json:"next_after_id"`
	}
	if err := json.Unmarshal(reply.Body.Bytes(), &body); err != nil || reply.Code != 200 || len(body.Events) != 1 {
		t.Fatal("risk event HTTP failed", err, reply.Code)
	}
	value := body.Events[0]
	if value.ID != orderHTTPID || value.Sequence != "9007199254740998" || value.Revision != "9007199254740995" || value.Recorded != "1" || value.Expires != "2000" || value.Reconciled || !value.Allowed || body.Next != orderHTTPID || strings.Contains(reply.Body.String(), `"market_feed_id"`) {
		t.Fatal("HTTP rounded or reinterpreted historical values/presence")
	}
	if client.request.StrategyId != 2 || client.request.AccountId != "risk.account" || client.request.InstrumentId != 1 || client.request.MarketDataSource != "okx" || client.request.Kind != "CHANGED" || client.request.ReasonCode != "READY" || !client.bounded || client.token != testSecret(1) || string(reply.Header().Peek("Cache-Control")) != "no-store" {
		t.Fatal("risk event HTTP lost native filter, session or deadline boundary")
	}
}

func TestRiskEventHTTPRejectsAmbiguousFiltersCookiesBodiesAndWrites(t *testing.T) {
	client := &riskEventHTTPClient{}
	hertz := riskEventHTTPFixture(t, client)
	for _, suffix := range []string{
		"?strategy_id=01", "?strategy_id=0", "?strategy_id=1&strategy_id=2", "?account_id=", "?account_id=space+account", "?account_id=a&account_id=b",
		"?instrument_id=01", "?instrument_id=-1", "?market_data_source=bitget", "?market_data_source=", "?kind=changed", "?reason_code=ready",
		"?kind=CHANGED&kind=REMOVED", "?reason_code=READY&reason_code=UNRECONCILED", "?limit=01", "?limit=101", "?after_id=-1", "?unexpected=1", "?bad=%zz",
		"/01", "/0", "/9223372036854775808", "/" + orderHTTPID + "?limit=1", "?" + strings.Repeat("x", 1025),
	} {
		if reply := perform(hertz, "GET", riskEventHTTPPath+suffix, "", orderHTTPCookie()); reply.Code != 400 {
			t.Fatal("invalid history request reached downstream", suffix, reply.Code)
		}
	}
	if reply := perform(hertz, "GET", riskEventHTTPPath, "{}", orderHTTPCookie()); reply.Code != 400 {
		t.Fatal("risk GET body accepted", reply.Code)
	}
	if reply := perform(hertz, "GET", riskEventHTTPPath, ""); reply.Code != 401 {
		t.Fatal("anonymous history read accepted", reply.Code)
	}
	if reply := perform(hertz, "GET", riskEventHTTPPath, "", orderHTTPCookie(), orderHTTPCookie()); reply.Code != 401 {
		t.Fatal("duplicate session proof accepted", reply.Code)
	}
	if reply := perform(hertz, "POST", riskEventHTTPPath, "{}", orderHTTPCookie()); reply.Code != 405 {
		t.Fatal("history read exposed a write method", reply.Code)
	}
	if reply := perform(riskEventHTTPFixture(t, nil), "GET", riskEventHTTPPath, "", orderHTTPCookie()); reply.Code != 404 {
		t.Fatal("disabled history queries exposed a route", reply.Code)
	}
	if client.calls != 0 {
		t.Fatal("invalid history requests touched downstream")
	}
}

func TestRiskEventHTTPRejectsFilteredMalformedAndUnknownSuccessfulData(t *testing.T) {
	client := &riskEventHTTPClient{}
	hertz := riskEventHTTPFixture(t, client)
	path := riskEventHTTPPath + "?limit=1&strategy_id=2&account_id=risk.account&instrument_id=1&market_data_source=okx&kind=CHANGED&reason_code=READY"
	for _, change := range []func(*strategyv1.ListRiskEventsReply){
		func(r *strategyv1.ListRiskEventsReply) { r.Events[0].StrategyId++ },
		func(r *strategyv1.ListRiskEventsReply) { r.Events[0].AccountId = "different.account" },
		func(r *strategyv1.ListRiskEventsReply) { r.Events[0].InstrumentId++ },
		func(r *strategyv1.ListRiskEventsReply) {
			r.Events[0].MarketDataSource = string(domain.MarketSourceBinance)
		},
		func(r *strategyv1.ListRiskEventsReply) { r.Events[0].Kind = string(domain.RiskSnapshotEventBaseline) },
		func(r *strategyv1.ListRiskEventsReply) { r.Events[0].ReasonCode = string(domain.RiskReasonBudget) },
		func(r *strategyv1.ListRiskEventsReply) { r.Events[0].MarketFeedId = proto.Int64(0) },
		func(r *strategyv1.ListRiskEventsReply) { r.Events[0].ExpiresAtMs = r.Events[0].ObservedAtMs },
		func(r *strategyv1.ListRiskEventsReply) { r.Events[0].ProtoReflect().SetUnknown([]byte{0xf8, 7, 1}) },
		func(r *strategyv1.ListRiskEventsReply) { r.ProtoReflect().SetUnknown([]byte{0xf8, 7, 1}) },
		func(r *strategyv1.ListRiskEventsReply) { r.NextAfterId++ },
		func(r *strategyv1.ListRiskEventsReply) { r.Events = append(r.Events, r.Events[0]) },
	} {
		client.mutate = change
		reply := perform(hertz, "GET", path, "", orderHTTPCookie())
		if reply.Code != 503 || strings.Contains(reply.Body.String(), "risk.account") || strings.Contains(reply.Body.String(), orderHTTPID) {
			t.Fatal("bad risk response leaked a partial successful event", reply.Code)
		}
	}
	client.mutate = func(r *strategyv1.ListRiskEventsReply) { r.Events[0].Id++ }
	if reply := perform(hertz, "GET", riskEventHTTPPath+"/"+orderHTTPID, "", orderHTTPCookie()); reply.Code != 503 {
		t.Fatal("risk detail returned another event identity", reply.Code)
	}
}
