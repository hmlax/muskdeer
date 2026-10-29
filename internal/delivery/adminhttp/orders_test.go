package adminhttp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

const orderHTTPPath = "/api/v1/strategies/9007199254740995/orders"
const orderHTTPID = "9007199254740993"

type orderHTTPClient struct {
	strategyHTTPClient
	request *strategyv1.ListStrategyOrdersRequest
	mutate  func(*strategyv1.ListStrategyOrdersReply)
}

func orderHTTPValue() *strategyv1.StrategyOrder {
	return &strategyv1.StrategyOrder{
		Id:               9007199254740993,
		ClientOrderId:    "http-order-query",
		StrategyId:       9007199254740995,
		AccountId:        "account-orders",
		InstrumentId:     1,
		ConfigVersion:    1,
		MarketDataSource: string(domain.MarketSourceBinance),
		Side:             string(domain.SideBuy),
		Price:            "100.000000000000000001",
		Quantity:         "2",
		FilledQuantity:   "0",
		ReservedNotional: "200.000000000000000002",
		TimeInForce:      string(domain.TimeInForceGTC),
		State:            string(domain.OrderStateUnknown),
		CancelState:      string(domain.CancelStateNone),
		Revision:         2,
		ErrorCode:        domain.IntentErrorOutcomeUnknown,
		DeadlineMs:       1700000001000,
		CreatedAtMs:      1700000000000,
		UpdatedAtMs:      1700000000000,
		Core: &strategyv1.StrategyOrderCore{
			Id:             5,
			State:          string(domain.OrderStatePartiallyFilled),
			CancelState:    string(domain.CancelStateNone),
			FilledQuantity: "1",
			FrozenAsset:    "USDT",
			FrozenAmount:   "100.100000000000000001001",
			QuoteFeeRate:   "0.001",
			Revision:       3,
			UpdatedAtMs:    1700000000001,
		},
	}
}

func (c *orderHTTPClient) ListStrategyOrders(ctx context.Context, request *strategyv1.ListStrategyOrdersRequest, _ ...grpc.CallOption) (*strategyv1.ListStrategyOrdersReply, error) {
	c.record(ctx, request.Session)
	c.request = request
	reply := &strategyv1.ListStrategyOrdersReply{
		Orders:      []*strategyv1.StrategyOrder{orderHTTPValue()},
		NextAfterId: 9007199254740993,
	}
	if c.mutate != nil {
		c.mutate(reply)
	}
	return reply, c.failure
}

func (c *orderHTTPClient) GetStrategyOrder(ctx context.Context, request *strategyv1.GetStrategyOrderRequest, _ ...grpc.CallOption) (*strategyv1.GetStrategyOrderReply, error) {
	page, err := c.ListStrategyOrders(ctx, &strategyv1.ListStrategyOrdersRequest{Session: request.Session})
	return &strategyv1.GetStrategyOrderReply{Order: page.Orders[0]}, err
}

func orderHTTPFixture(t *testing.T, client strategyv1.StrategyOrderQueryClient) *server.Hertz {
	t.Helper()
	routes, err := New(&authFixture{}, testOrigin, true)
	if err != nil {
		t.Fatal(err)
	}
	routes.orders = client
	hertz := server.New()
	routes.Register(hertz)
	return hertz
}

func orderHTTPCookie() ut.Header {
	return ut.Header{
		Key:   "Cookie",
		Value: secureSessionCookie + "=" + testSecret(1),
	}
}

func TestOrderHTTPPreservesExactStringTypesAndIndependentCoreFacts(t *testing.T) {
	client := &orderHTTPClient{}
	hertz := orderHTTPFixture(t, client)
	for _, path := range []string{orderHTTPPath + "?limit=1&after_id=9007199254740992&state=" + string(domain.OrderStateUnknown) + "&open_only=true", orderHTTPPath + "/" + orderHTTPID} {
		reply := perform(hertz, "GET", path, "", orderHTTPCookie())
		var body struct {
			Orders []struct {
				ID       string `json:"id"`
				Price    string `json:"price"`
				Reserved string `json:"reserved_notional"`
				Core     struct {
					Frozen string `json:"frozen_amount"`
				} `json:"core"`
			} `json:"orders"`
			Next string `json:"next_after_id"`
		}
		if reply.Code != 200 || !client.bounded || client.token != testSecret(1) || strings.Contains(reply.Body.String(), testSecret(1)) || string(reply.Header().Peek("Cache-Control")) != "no-store" {
			t.Fatal("order HTTP lost current cookie, native budget or no-store", reply.Code)
		}
		if strings.Contains(path, "?") {
			if err := json.Unmarshal(reply.Body.Bytes(), &body); err != nil || body.Next != orderHTTPID || len(body.Orders) != 1 || body.Orders[0].ID != orderHTTPID || body.Orders[0].Price != "100.000000000000000001" || body.Orders[0].Reserved != "200.000000000000000002" || body.Orders[0].Core.Frozen != "100.100000000000000001001" || !client.request.OpenOnly || client.request.State != string(domain.OrderStateUnknown) {
				t.Fatal("order HTTP rounded identities or decimals, or lost filters", err)
			}
		}
	}
}

func TestOrderHTTPRejectsAmbiguityMissingProofAndWrites(t *testing.T) {
	client := &orderHTTPClient{}
	hertz := orderHTTPFixture(t, client)
	for _, path := range []string{
		orderHTTPPath + "?state=", orderHTTPPath + "?state=unknown", orderHTTPPath + "?state=LIVE&state=UNKNOWN",
		orderHTTPPath + "?open_only=1", orderHTTPPath + "?open_only=True", orderHTTPPath + "?open_only=true&open_only=false",
		orderHTTPPath + "?state=FILLED&open_only=true", orderHTTPPath + "?limit=101", orderHTTPPath + "?limit=01",
		orderHTTPPath + "?after_id=-1", orderHTTPPath + "?after_id=9223372036854775808", orderHTTPPath + "?session=forged",
		orderHTTPPath + "/01", orderHTTPPath + "/0", orderHTTPPath + "/9223372036854775808", orderHTTPPath + "/" + orderHTTPID + "?limit=1",
		"/api/v1/strategies/01/orders", orderHTTPPath + "?" + strings.Repeat("x", 1025),
	} {
		if reply := perform(hertz, "GET", path, "", orderHTTPCookie()); reply.Code != 400 {
			t.Fatal("ambiguous order request reached RPC", path, reply.Code)
		}
	}
	if reply := perform(hertz, "GET", orderHTTPPath, "{}", orderHTTPCookie()); reply.Code != 400 {
		t.Fatal("GET body reached order RPC", reply.Code)
	}
	if reply := perform(hertz, "GET", orderHTTPPath, ""); reply.Code != 401 {
		t.Fatal("missing cookie reached order RPC", reply.Code)
	}
	if reply := perform(hertz, "GET", orderHTTPPath, "", orderHTTPCookie(), orderHTTPCookie()); reply.Code != 401 {
		t.Fatal("duplicate cookie reached order RPC", reply.Code)
	}
	if reply := perform(hertz, "POST", orderHTTPPath, "{}", orderHTTPCookie()); reply.Code != 405 || client.calls != 0 {
		t.Fatal("order query exposed write or called invalid requests", reply.Code)
	}
	if reply := perform(orderHTTPFixture(t, nil), "GET", orderHTTPPath, "", orderHTTPCookie()); reply.Code != 404 {
		t.Fatal("disabled query exposed orders", reply.Code)
	}
}

func TestOrderHTTPRejectsSuccessfulWrongScopeCursorAndMoney(t *testing.T) {
	client := &orderHTTPClient{}
	hertz := orderHTTPFixture(t, client)
	for _, mutate := range []func(*strategyv1.ListStrategyOrdersReply){
		func(p *strategyv1.ListStrategyOrdersReply) { p.Orders[0].StrategyId++ },
		func(p *strategyv1.ListStrategyOrdersReply) { p.NextAfterId++ },
		func(p *strategyv1.ListStrategyOrdersReply) { p.Orders = append(p.Orders, p.Orders[0]) },
		func(p *strategyv1.ListStrategyOrdersReply) { p.Orders[0].Price = "NaN" },
		func(p *strategyv1.ListStrategyOrdersReply) { p.Orders[0].Quantity = "2.0000000000000000001" },
		func(p *strategyv1.ListStrategyOrdersReply) { p.Orders[0].Core.FilledQuantity = "3" },
		func(p *strategyv1.ListStrategyOrdersReply) { p.Orders[0].Core.QuoteFeeRate = "1" },
		func(p *strategyv1.ListStrategyOrdersReply) {
			p.Orders[0].ConfirmedCoreId = 7
			p.Orders[0].ConfirmedCoreRevision = 1
		},
		func(p *strategyv1.ListStrategyOrdersReply) {
			p.Orders[0].Core.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
		},
		func(p *strategyv1.ListStrategyOrdersReply) { p.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01}) },
	} {
		client.mutate = mutate
		if reply := perform(hertz, "GET", orderHTTPPath+"?limit=1", "", orderHTTPCookie()); reply.Code != 503 || strings.Contains(reply.Body.String(), "client_order_id") {
			t.Fatal("invalid successful order page escaped", reply.Code)
		}
	}
	client.mutate = nil
	if reply := perform(hertz, "GET", orderHTTPPath+"/1", "", orderHTTPCookie()); reply.Code != 503 {
		t.Fatal("order detail accepted wrong identity", reply.Code)
	}
	for _, test := range []struct {
		code codes.Code
		want int
	}{
		{
			code: codes.Unauthenticated,
			want: 401,
		},
		{
			code: codes.PermissionDenied,
			want: 403,
		},
		{
			code: codes.NotFound,
			want: 404,
		},
		{
			code: codes.Unavailable,
			want: 503,
		},
	} {
		client.failure = status.Error(test.code, "private-order-storage-details")
		if reply := perform(hertz, "GET", orderHTTPPath, "", orderHTTPCookie()); reply.Code != test.want || strings.Contains(reply.Body.String(), "private-order-storage-details") || strings.Contains(reply.Body.String(), "client_order_id") {
			t.Fatal("order failure leaked partial success or private cause", reply.Code)
		}
	}
}
