package adminhttp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"google.golang.org/grpc"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

const tradeHTTPPath = "/api/v1/strategies/9007199254740995/trades"

type tradeHTTPClient struct {
	strategyHTTPClient
	request *strategyv1.ListStrategyTradesRequest
	mutate  func(*strategyv1.ListStrategyTradesReply)
}

func tradeHTTPValue() *strategyv1.StrategyTrade {
	id := int64(9007199254740997)
	return &strategyv1.StrategyTrade{
		Id:               9007199254740993,
		ExecutionVenue:   domain.ExecutionVenueSimulator,
		TradeId:          "http-trade-query",
		StrategyId:       9007199254740995,
		AccountId:        "account-trades",
		InstrumentId:     1,
		ConfigVersion:    1,
		MarketDataSource: string(domain.MarketSourceBinance),
		CoreOrderId:      9007199254740996,
		ClientOrderId:    "http-trade-client",
		OrderId:          &id,
		Side:             string(domain.SideBuy),
		Price:            "99.99",
		Quantity:         "1",
		QuoteNotional:    "99.99",
		FeeAmount:        "0.12344444333344436544",
		FeeAsset:         "USDT",
		RecordedAtMs:     1700000000000,
		PortfolioFact: &strategyv1.TradePortfolioFact{
			Id:           9007199254740998,
			RecordedAtMs: 1700000000001,
		},
	}
}

func (c *tradeHTTPClient) ListStrategyTrades(ctx context.Context, request *strategyv1.ListStrategyTradesRequest, _ ...grpc.CallOption) (*strategyv1.ListStrategyTradesReply, error) {
	c.record(ctx, request.Session)
	c.request = request
	reply := &strategyv1.ListStrategyTradesReply{
		Trades:      []*strategyv1.StrategyTrade{tradeHTTPValue()},
		NextAfterId: 9007199254740993,
	}
	if c.mutate != nil {
		c.mutate(reply)
	}
	return reply, c.failure
}

func (c *tradeHTTPClient) GetStrategyTrade(ctx context.Context, request *strategyv1.GetStrategyTradeRequest, _ ...grpc.CallOption) (*strategyv1.GetStrategyTradeReply, error) {
	page, err := c.ListStrategyTrades(ctx, &strategyv1.ListStrategyTradesRequest{Session: request.Session})
	return &strategyv1.GetStrategyTradeReply{Trade: page.Trades[0]}, err
}

func tradeHTTPFixture(t *testing.T, client strategyv1.StrategyTradeQueryClient) *server.Hertz {
	t.Helper()
	routes, err := New(&authFixture{}, testOrigin, true)
	if err != nil {
		t.Fatal(err)
	}
	routes.trades = client
	hertz := server.New()
	routes.Register(hertz)
	return hertz
}

func TestTradeHTTPPreservesStringIdentityExactFeeAndOptionalFacts(t *testing.T) {
	client := &tradeHTTPClient{}
	hertz := tradeHTTPFixture(t, client)
	reply := perform(hertz, "GET", tradeHTTPPath+"?limit=1&after_id=9007199254740992&order_id=9007199254740997&side=BUY", "", orderHTTPCookie())
	var body struct {
		Trades []struct {
			ID      string `json:"id"`
			OrderID string `json:"order_id"`
			Fee     string `json:"fee_amount"`
			Fact    struct {
				ID string `json:"id"`
			} `json:"portfolio_fact"`
		} `json:"trades"`
		Next string `json:"next_after_id"`
	}
	if err := json.Unmarshal(reply.Body.Bytes(), &body); err != nil || reply.Code != 200 || len(body.Trades) != 1 || body.Trades[0].ID != orderHTTPID || body.Trades[0].OrderID != "9007199254740997" || body.Trades[0].Fee != "0.12344444333344436544" || body.Trades[0].Fact.ID != "9007199254740998" || body.Next != orderHTTPID || client.request.OrderId != 9007199254740997 || client.request.Side != string(domain.SideBuy) || !client.bounded || client.token != testSecret(1) || string(reply.Header().Peek("Cache-Control")) != "no-store" {
		t.Fatal("trade HTTP lost precise data, scope, timeout or no-store", err, reply.Code)
	}
	client.mutate = func(page *strategyv1.ListStrategyTradesReply) {
		page.Trades[0].OrderId, page.Trades[0].PortfolioFact = nil, nil
	}
	reply = perform(hertz, "GET", tradeHTTPPath+"/"+orderHTTPID, "", orderHTTPCookie())
	// ProtoJSON omits an absent proto3 optional scalar, while an unpopulated
	// message is null. Neither absence may become a synthetic zero identity.
	if reply.Code != 200 || strings.Contains(reply.Body.String(), `"order_id"`) || !strings.Contains(reply.Body.String(), `"portfolio_fact":null`) || strings.Contains(reply.Body.String(), testSecret(1)) {
		t.Fatal("unlinked/uncollected facts were invented or omitted ambiguously", reply.Code, reply.Body.String())
	}
}

func TestTradeHTTPRejectsAmbiguityAuthorityAndWriteMethods(t *testing.T) {
	client := &tradeHTTPClient{}
	hertz := tradeHTTPFixture(t, client)
	for _, path := range []string{
		tradeHTTPPath + "?order_id=0", tradeHTTPPath + "?order_id=01", tradeHTTPPath + "?order_id=1&order_id=2", tradeHTTPPath + "?order_id=9223372036854775808",
		tradeHTTPPath + "?side=", tradeHTTPPath + "?side=buy", tradeHTTPPath + "?side=BUY&side=SELL", tradeHTTPPath + "?state=LIVE",
		tradeHTTPPath + "?limit=101", tradeHTTPPath + "?after_id=-1", tradeHTTPPath + "?limit=01", tradeHTTPPath + "?session=forged",
		tradeHTTPPath + "/0", tradeHTTPPath + "/01", tradeHTTPPath + "/" + orderHTTPID + "?limit=1", "/api/v1/strategies/01/trades",
		tradeHTTPPath + "?" + strings.Repeat("x", 1025),
	} {
		if reply := perform(hertz, "GET", path, "", orderHTTPCookie()); reply.Code != 400 {
			t.Fatal("ambiguous trade query reached RPC", path, reply.Code)
		}
	}
	if reply := perform(hertz, "GET", tradeHTTPPath, "{}", orderHTTPCookie()); reply.Code != 400 {
		t.Fatal("trade GET body accepted", reply.Code)
	}
	if reply := perform(hertz, "GET", tradeHTTPPath, ""); reply.Code != 401 {
		t.Fatal("missing current trade session accepted", reply.Code)
	}
	if reply := perform(hertz, "GET", tradeHTTPPath, "", orderHTTPCookie(), orderHTTPCookie()); reply.Code != 401 {
		t.Fatal("duplicate trade session accepted", reply.Code)
	}
	if reply := perform(hertz, "POST", tradeHTTPPath, "{}", orderHTTPCookie()); reply.Code != 405 {
		t.Fatal("trade query exposed a write method", reply.Code)
	}
	if reply := perform(tradeHTTPFixture(t, nil), "GET", tradeHTTPPath, "", orderHTTPCookie()); reply.Code != 404 {
		t.Fatal("disabled trade queries exposed a route", reply.Code)
	}
	if client.calls != 0 {
		t.Fatal("invalid trade query reached downstream")
	}
}

func TestTradeHTTPRejectsCorruptSuccessfulResponsesWithoutPartialData(t *testing.T) {
	client := &tradeHTTPClient{}
	hertz := tradeHTTPFixture(t, client)
	for _, change := range []func(*strategyv1.ListStrategyTradesReply){
		func(r *strategyv1.ListStrategyTradesReply) { r.Trades[0].StrategyId++ },
		func(r *strategyv1.ListStrategyTradesReply) { r.Trades[0].Price = "1e2147483647" },
		func(r *strategyv1.ListStrategyTradesReply) { r.Trades[0].QuoteNotional = "100" },
		func(r *strategyv1.ListStrategyTradesReply) { r.Trades[0].FeeAmount = "-1" },
		func(r *strategyv1.ListStrategyTradesReply) { r.Trades[0].GetPortfolioFact().RecordedAtMs = 1 },
		func(r *strategyv1.ListStrategyTradesReply) {
			r.Trades[0].GetPortfolioFact().ProtoReflect().SetUnknown([]byte{0xf8, 7, 1})
		},
		func(r *strategyv1.ListStrategyTradesReply) { r.Trades[0].OrderId = nil },
		func(r *strategyv1.ListStrategyTradesReply) { r.NextAfterId++ },
		func(r *strategyv1.ListStrategyTradesReply) { r.Trades = append(r.Trades, r.Trades[0]) },
	} {
		client.mutate = change
		reply := perform(hertz, "GET", tradeHTTPPath+"?limit=1&order_id=9007199254740997", "", orderHTTPCookie())
		if reply.Code != 503 || strings.Contains(reply.Body.String(), "99.99") || strings.Contains(reply.Body.String(), "http-trade-query") {
			t.Fatal("bad trade success leaked partial facts", reply.Code, reply.Body.String())
		}
	}
	client.mutate = func(page *strategyv1.ListStrategyTradesReply) { page.Trades[0].Id++ }
	if reply := perform(hertz, "GET", tradeHTTPPath+"/"+orderHTTPID, "", orderHTTPCookie()); reply.Code != 503 || strings.Contains(reply.Body.String(), "99.99") {
		t.Fatal("different trade identity escaped HTTP detail binding", reply.Code)
	}
}
