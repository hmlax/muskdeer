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

const creationHTTPBody = `{"instance_key":"http-grid","account_id":"http-account","instrument_id":"9007199254740993","code_version":"http-test","market_data_source":"` + string(domain.MarketSourceOKX) + `","market_mode":"` + string(domain.MarketModeReplay) + `","parameters":{"grid":{"lower_price":"98.123456789012345678","upper_price":"102","grid_count":5,"total_budget":"50","per_order_notional":"10","inventory_limit":"20","max_market_age_ms":"500"}},"reason":"explicit definition"}`

type creationHTTPClient struct {
	strategyHTTPClient
	request *strategyv1.CreateStrategyRequest
}

func (c *creationHTTPClient) CreateStrategy(ctx context.Context, request *strategyv1.CreateStrategyRequest, _ ...grpc.CallOption) (*strategyv1.CreationReceipt, error) {
	c.record(ctx, request.Session)
	c.request = request
	return &strategyv1.CreationReceipt{
		Id:               9007199254740993,
		ActorUserId:      1,
		StrategyId:       9007199254740994,
		ConfigVersion:    1,
		Revision:         1,
		MarketDataSource: request.Input.MarketDataSource,
		Reason:           request.Input.Reason,
	}, c.failure
}

func creationHTTPFixture(t *testing.T, client strategyv1.StrategyCreationClient) *server.Hertz {
	t.Helper()
	routes, err := New(&authFixture{}, testOrigin, true)
	if err != nil {
		t.Fatal(err)
	}
	routes.creation = client
	hertz := server.New()
	routes.Register(hertz)
	return hertz
}

func TestCreationHTTPKeepsStringIdentityDecimalAndIndependentProofs(t *testing.T) {
	client := &creationHTTPClient{}
	hertz := creationHTTPFixture(t, client)
	reply := perform(hertz, "POST", "/api/v1/strategies", creationHTTPBody, controlHTTPHeaders()...)
	var body struct {
		ID         string `json:"id"`
		StrategyID string `json:"strategy_id"`
	}
	if err := json.Unmarshal(reply.Body.Bytes(), &body); err != nil || reply.Code != 201 || body.ID != "9007199254740993" || body.StrategyID != "9007199254740994" {
		t.Fatal("creation lost exact response identity", err, reply.Code)
	}
	if !client.bounded || client.request.Input.InstrumentId != 9007199254740993 || client.request.Input.Parameters.GetGrid().LowerPrice != "98.123456789012345678" || client.request.CsrfToken != testSecret(2) || client.request.Session.Token != testSecret(1) || client.request.IdempotencyKey != "http-control-original-key" || string(reply.Header().Peek("Location")) != "/api/v1/strategies/9007199254740994" {
		t.Fatal("creation lost typed business input or independent proof")
	}
	client.failure = status.Error(codes.Aborted, "private-data")
	if reply := perform(hertz, "POST", "/api/v1/strategies", creationHTTPBody, controlHTTPHeaders()...); reply.Code != 409 || strings.Contains(reply.Body.String(), "private-data") {
		t.Fatal("creation conflict was lost or leaked details")
	}
}

func TestCreationHTTPRejectsNestedAmbiguityAndNumericDecimalBeforeRPC(t *testing.T) {
	client := &creationHTTPClient{}
	hertz := creationHTTPFixture(t, client)
	for _, body := range []string{
		`{}`, `null`, creationHTTPBody + `{}`,
		strings.Replace(creationHTTPBody, `"instrument_id":"9007199254740993"`, `"instrument_id":9007199254740993`, 1),
		strings.Replace(creationHTTPBody, `"lower_price":"98.123456789012345678"`, `"lower_price":98.1`, 1),
		strings.Replace(creationHTTPBody, `"lower_price":"98.123456789012345678"`, `"lower_price":"98","lower_price":"99"`, 1),
		strings.Replace(creationHTTPBody, `"reason":"explicit definition"`, `"reason":"one","reason":"two"`, 1),
		strings.Replace(creationHTTPBody, `"grid_count":5`, `"grid_count":5,"unknown":true`, 1),
		strings.Replace(creationHTTPBody, `"instance_key":"http-grid"`, `"session":"forged","instance_key":"http-grid"`, 1),
	} {
		if reply := perform(hertz, "POST", "/api/v1/strategies", body, controlHTTPHeaders()...); reply.Code != 400 {
			t.Fatal("ambiguous creation accepted", reply.Code)
		}
	}
	for index, expected := range []int{403, 401, 403, 400} {
		headers := controlHTTPHeaders()
		headers = append(headers[:index], headers[index+1:]...)
		if reply := perform(hertz, "POST", "/api/v1/strategies", creationHTTPBody, headers...); reply.Code != expected {
			t.Fatal("creation accepted missing independent proof", index, reply.Code)
		}
	}
	if client.calls != 0 {
		t.Fatal("invalid creation caused RPC")
	}
}
