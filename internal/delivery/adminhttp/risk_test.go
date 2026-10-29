package adminhttp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

const riskHTTPPath = "/api/v1/strategies/9007199254740994/risk"
const riskHTTPBody = `{"action":"` + string(domain.AdminRiskBlock) + `","expected_revision":"9007199254740993","reason":"  explicit risk block  "}`

type riskHTTPClient struct {
	strategyHTTPClient
	request       *strategyv1.ChangeStrategyRiskRequest
	mutateReceipt func(*strategyv1.StrategyRiskReceipt)
	mutateState   func(*strategyv1.StrategyRiskState)
}

func (c *riskHTTPClient) GetStrategyRisk(ctx context.Context, request *strategyv1.GetStrategyRiskRequest, _ ...grpc.CallOption) (*strategyv1.StrategyRiskState, error) {
	c.record(ctx, request.Session)
	value := &strategyv1.StrategyRiskState{
		StrategyId:             request.StrategyId,
		Enabled:                false,
		Revision:               9007199254740993,
		QuoteAsset:             "USDT",
		LimitNotional:          "300.000000000000000001",
		ReservedNotional:       "123.000000000000000001",
		State:                  string(domain.StrategyStateRunning),
		DesiredState:           string(domain.StrategyStatePaused),
		RequestedConfigVersion: 9007199254740995,
		MarketDataSource:       string(domain.MarketSourceOKX),
		UpdatedAtMs:            1700000000000,
	}
	if c.mutateState != nil {
		c.mutateState(value)
	}
	return value, c.failure
}

func (c *riskHTTPClient) ChangeStrategyRisk(ctx context.Context, request *strategyv1.ChangeStrategyRiskRequest, _ ...grpc.CallOption) (*strategyv1.StrategyRiskReceipt, error) {
	c.record(ctx, request.Session)
	c.request = request
	value := &strategyv1.StrategyRiskReceipt{
		Id:                    9007199254740993,
		ActorUserId:           1,
		StrategyId:            request.StrategyId,
		Action:                request.Input.Action,
		PreviousEnabled:       true,
		Enabled:               domain.AdminRiskAction(request.Input.Action) == domain.AdminRiskUnblock,
		PreviousRevision:      request.Input.ExpectedRevision,
		Revision:              request.Input.ExpectedRevision + 1,
		ExpectedConfigVersion: request.Input.ExpectedConfigVersion,
		ExpectedDesiredState:  request.Input.ExpectedDesiredState,
		Reason:                request.Input.Reason,
		CreatedAtMs:           1700000000000,
	}
	if c.mutateReceipt != nil {
		c.mutateReceipt(value)
	}
	return value, c.failure
}

func riskHTTPFixture(t *testing.T, reader strategyv1.StrategyRiskQueryClient, writer strategyv1.StrategyRiskControlClient) *server.Hertz {
	t.Helper()
	routes, err := New(&authFixture{}, testOrigin, true)
	if err != nil {
		t.Fatal(err)
	}
	routes.riskQueries, routes.riskControls = reader, writer
	hertz := server.New()
	routes.Register(hertz)
	return hertz
}

func TestRiskHTTPPreservesAmountsVersionsAndIndependentProof(t *testing.T) {
	client := &riskHTTPClient{}
	hertz := riskHTTPFixture(t, client, client)
	reply := perform(hertz, "GET", riskHTTPPath, "", controlHTTPHeaders()...)
	var state struct {
		StrategyID string `json:"strategy_id"`
		Revision   string `json:"revision"`
		Limit      string `json:"limit_notional"`
		Reserved   string `json:"reserved_notional"`
		Version    string `json:"requested_config_version"`
	}
	if err := json.Unmarshal(reply.Body.Bytes(), &state); err != nil || reply.Code != 200 || state.StrategyID != "9007199254740994" || state.Revision != "9007199254740993" || state.Version != "9007199254740995" || state.Limit != "300.000000000000000001" || state.Reserved != "123.000000000000000001" {
		t.Fatal("risk HTTP state changed exact string types", err, reply.Code)
	}
	reply = perform(hertz, "POST", riskHTTPPath, riskHTTPBody, controlHTTPHeaders()...)
	var receipt struct {
		ID       string `json:"id"`
		Revision string `json:"revision"`
		Enabled  bool   `json:"enabled"`
	}
	if err := json.Unmarshal(reply.Body.Bytes(), &receipt); err != nil || reply.Code != 200 || receipt.ID != "9007199254740993" || receipt.Revision != "9007199254740994" || receipt.Enabled || string(reply.Header().Peek("Location")) != riskHTTPPath {
		t.Fatal("risk HTTP receipt changed identity or flag", err, reply.Code)
	}
	if client.token != testSecret(1) || client.request.CsrfToken != testSecret(2) || client.request.IdempotencyKey != "http-control-original-key" || client.request.Input.Reason != "explicit risk block" || !client.bounded || strings.Contains(reply.Body.String(), testSecret(1)) || string(reply.Header().Peek("Cache-Control")) != "no-store" {
		t.Fatal("risk HTTP lost owned proof, deadline, normalization or cache policy")
	}
	restore := `{"action":"` + string(domain.AdminRiskUnblock) + `","expected_revision":"9007199254740993","expected_config_version":"9007199254740995","expected_desired_state":"` + string(domain.StrategyStatePaused) + `","reason":"explicit restore"}`
	if reply := perform(hertz, "POST", riskHTTPPath, restore, controlHTTPHeaders()...); reply.Code != 200 || client.request.Input.ExpectedConfigVersion != 9007199254740995 || client.request.Input.ExpectedDesiredState != string(domain.StrategyStatePaused) {
		t.Fatal("risk restore lost explicit confirmation fields", reply.Code)
	}
}

func TestRiskHTTPRejectsAmbiguityMissingProofAndDisabledControl(t *testing.T) {
	client := &riskHTTPClient{}
	hertz := riskHTTPFixture(t, client, client)
	for _, body := range []string{
		`{}`, `null`, riskHTTPBody + `{}`,
		strings.Replace(riskHTTPBody, `"expected_revision":"9007199254740993"`, `"expected_revision":9007199254740993`, 1),
		strings.Replace(riskHTTPBody, `"expected_revision":"9007199254740993"`, `"expected_revision":"01"`, 1),
		strings.Replace(riskHTTPBody, `"expected_revision":"9007199254740993"`, `"expected_revision":"9223372036854775807"`, 1),
		strings.Replace(riskHTTPBody, `"reason":`, `"reason":"duplicate","reason":`, 1),
		strings.Replace(riskHTTPBody, `"reason":`, `"session":"forged","reason":`, 1),
		strings.Replace(riskHTTPBody, `"reason":"  explicit risk block  "`, `"reason":null`, 1),
		strings.Replace(riskHTTPBody, `"reason":`, `"expected_config_version":"1","reason":`, 1),
		strings.Replace(riskHTTPBody, string(domain.AdminRiskBlock), string(domain.AdminRiskUnblock), 1),
		strings.Repeat(" ", maximumControlBytes+1),
	} {
		if reply := perform(hertz, "POST", riskHTTPPath, body, controlHTTPHeaders()...); reply.Code != 400 {
			t.Fatal("ambiguous risk body reached RPC", reply.Code)
		}
	}
	for index, expected := range []int{403, 401, 403, 400} {
		headers := controlHTTPHeaders()
		headers = append(headers[:index], headers[index+1:]...)
		if reply := perform(hertz, "POST", riskHTTPPath, riskHTTPBody, headers...); reply.Code != expected {
			t.Fatal("risk mutation accepted missing independent proof", index, reply.Code)
		}
	}
	for _, path := range []string{riskHTTPPath + "?unexpected=1", "/api/v1/strategies/01/risk"} {
		if reply := perform(hertz, "GET", path, "", controlHTTPHeaders()...); reply.Code != 400 {
			t.Fatal("ambiguous risk read reached RPC", reply.Code)
		}
	}
	if client.calls != 0 {
		t.Fatal("invalid risk request reached RPC")
	}
	if reply := perform(riskHTTPFixture(t, client, nil), "POST", riskHTTPPath, riskHTTPBody, controlHTTPHeaders()...); reply.Code != 405 {
		t.Fatal("query-only risk API exposed writes", reply.Code)
	}
	if reply := perform(riskHTTPFixture(t, nil, nil), "GET", riskHTTPPath, "", controlHTTPHeaders()...); reply.Code != 404 {
		t.Fatal("disabled risk API exposed reads", reply.Code)
	}
}

func TestRiskHTTPRejectsMismatchedSuccessAndClassifiesRecovery(t *testing.T) {
	client := &riskHTTPClient{}
	hertz := riskHTTPFixture(t, client, client)
	for _, mutate := range []func(*strategyv1.StrategyRiskState){
		func(r *strategyv1.StrategyRiskState) { r.StrategyId++ },
		func(r *strategyv1.StrategyRiskState) { r.ReservedNotional = "301" },
		func(r *strategyv1.StrategyRiskState) { r.LimitNotional = "NaN" },
		func(r *strategyv1.StrategyRiskState) { r.DesiredState = string(domain.StrategyStateCreated) },
	} {
		client.mutateState = mutate
		if reply := perform(hertz, "GET", riskHTTPPath, "", controlHTTPHeaders()...); reply.Code != 503 || strings.Contains(reply.Body.String(), "strategy_id") {
			t.Fatal("invalid successful risk state escaped", reply.Code)
		}
	}
	for _, mutate := range []func(*strategyv1.StrategyRiskReceipt){
		func(r *strategyv1.StrategyRiskReceipt) { r.StrategyId++ },
		func(r *strategyv1.StrategyRiskReceipt) { r.Revision++ },
		func(r *strategyv1.StrategyRiskReceipt) { r.Enabled = true },
		func(r *strategyv1.StrategyRiskReceipt) { r.ExpectedConfigVersion = 1 },
		func(r *strategyv1.StrategyRiskReceipt) { r.Reason = "another request" },
	} {
		client.mutateReceipt = mutate
		if reply := perform(hertz, "POST", riskHTTPPath, riskHTTPBody, controlHTTPHeaders()...); reply.Code != 503 || strings.Contains(reply.Body.String(), "actor_user_id") {
			t.Fatal("invalid successful risk receipt escaped", reply.Code)
		}
	}
	client.failure = status.Error(codes.FailedPrecondition, "private-risk-diagnostics")
	reply := perform(hertz, "POST", riskHTTPPath, riskHTTPBody, controlHTTPHeaders()...)
	var failure errorReply
	if err := json.Unmarshal(reply.Body.Bytes(), &failure); err != nil || reply.Code != 409 || failure.Code != domain.AdminRiskRecoveryDeniedCode || failure.Message != domain.MessageAdminRiskRecoveryDenied {
		t.Fatal("risk recovery denial lost its fixed public classification", err)
	}
	client.failure = status.Error(codes.Aborted, "private-risk-diagnostics")
	reply = perform(hertz, "POST", riskHTTPPath, riskHTTPBody, controlHTTPHeaders()...)
	if reply.Code != 409 || strings.Contains(reply.Body.String(), "private-risk-diagnostics") {
		t.Fatal("risk conflict leaked private details", reply.Code)
	}
}
