package strategygrpc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
)

type orderRPCStore struct {
	calls  int
	token  string
	filter domain.AdminOrderFilter
	page   domain.AdminOrderPage
	err    error
}

func (s *orderRPCStore) List(_ context.Context, token string, filter domain.AdminOrderFilter) (domain.AdminOrderPage, error) {
	s.calls++
	s.token, s.filter = token, filter
	return s.page, s.err
}

func (s *orderRPCStore) Detail(_ context.Context, token string, _, _ int64) (domain.AdminOrder, error) {
	s.calls++
	s.token = token
	return s.page.Items[0], s.err
}

func orderRPCFixture() domain.AdminOrder {
	now := time.UnixMilli(1700000000000)
	return domain.AdminOrder{
		ID:                    9007199254740993,
		ClientOrderID:         "order-query-client",
		StrategyID:            9007199254740995,
		AccountID:             "account-orders",
		InstrumentID:          1,
		ConfigVersion:         1,
		Source:                domain.MarketSourceBinance,
		Side:                  domain.SideBuy,
		Price:                 decimal.RequireFromString("100.000000000000000001"),
		Quantity:              decimal.NewFromInt(2),
		FilledQuantity:        decimal.Zero,
		ReservedNotional:      decimal.RequireFromString("200.000000000000000002"),
		TimeInForce:           domain.TimeInForceGTC,
		State:                 domain.OrderStateLive,
		CancelState:           domain.CancelStatePending,
		ConfirmedCoreID:       5,
		ConfirmedCoreRevision: 1,
		Revision:              2,
		Deadline:              now.Add(time.Second),
		CreatedAt:             now,
		UpdatedAt:             now,
		Core: &domain.CoreOrder{
			ID:             5,
			ClientOrderID:  "order-query-client",
			StrategyID:     9007199254740995,
			AccountID:      "account-orders",
			InstrumentID:   1,
			ConfigVersion:  1,
			Side:           domain.SideBuy,
			Price:          decimal.RequireFromString("100.000000000000000001"),
			Quantity:       decimal.NewFromInt(2),
			FilledQuantity: decimal.NewFromInt(1),
			TimeInForce:    domain.TimeInForceGTC,
			State:          domain.OrderStatePartiallyFilled,
			CancelState:    domain.CancelStatePending,
			FrozenAsset:    "USDT",
			FrozenAmount:   decimal.RequireFromString("100.100000000000000001001"),
			QuoteFeeRate:   decimal.RequireFromString("0.001"),
			Revision:       3,
		},
		CoreUpdatedAt: now.Add(time.Millisecond),
	}
}

func orderRPCRequest() *strategyv1.ListStrategyOrdersRequest {
	return &strategyv1.ListStrategyOrdersRequest{
		Session:    &strategyv1.AdminSession{Token: testSessionToken},
		StrategyId: 9007199254740995,
		Limit:      1,
		OpenOnly:   true,
	}
}

func TestOrderGRPCPreservesHistoricalSourceAndIndependentObservations(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = verified(ctx, rpcsecurity.ServiceAPI)
	store := &orderRPCStore{page: domain.AdminOrderPage{Items: []domain.AdminOrder{orderRPCFixture()}}}
	reader, err := NewOrderQuery(store)
	if err != nil {
		t.Fatal(err)
	}
	page, err := reader.ListStrategyOrders(ctx, orderRPCRequest())
	if err != nil || len(page.Orders) != 1 || page.Orders[0].Id != 9007199254740993 || page.Orders[0].Price != "100.000000000000000001" || page.Orders[0].ReservedNotional != "200.000000000000000002" || page.Orders[0].Core.FrozenAmount != "100.100000000000000001001" || page.Orders[0].FilledQuantity != "0" || page.Orders[0].Core.FilledQuantity != "1" || page.Orders[0].ConfirmedCoreRevision != 1 || page.Orders[0].Core.Revision != 3 || store.token != testSessionToken || !store.filter.OpenOnly {
		t.Fatal("order RPC lost exact amounts, proof or independent observations", err)
	}
	value := &store.page.Items[0]
	value.State, value.ErrorCode = domain.OrderStateUnknown, domain.IntentErrorOutcomeUnknown
	value.ConfirmedCoreID, value.ConfirmedCoreRevision = 0, 0
	detail, err := reader.GetStrategyOrder(ctx, &strategyv1.GetStrategyOrderRequest{
		Session:    orderRPCRequest().Session,
		StrategyId: value.StrategyID,
		OrderId:    value.ID,
	})
	if err != nil || detail.Order.Core == nil || detail.Order.State != string(domain.OrderStateUnknown) || detail.Order.MarketDataSource != string(domain.MarketSourceBinance) {
		t.Fatal("independently observed acceptance overwrote historical OMS state", err)
	}
	value.Core = nil
	if page, err := reader.ListStrategyOrders(ctx, orderRPCRequest()); err != nil || page.Orders[0].Core != nil || page.Orders[0].ReservedNotional == "0" {
		t.Fatal("missing core observation became a no-effect or release proof", err)
	}
}

func TestOrderGRPCRejectsWrongAuthorityUnboundedAndAmbiguousRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	store := &orderRPCStore{}
	reader, err := NewOrderQuery(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ListStrategyOrders(verified(ctx, rpcsecurity.ServicePortfolio), orderRPCRequest()); status.Code(err) != codes.PermissionDenied {
		t.Fatal("non-API certificate read orders", err)
	}
	if _, err := reader.ListStrategyOrders(verified(t.Context(), rpcsecurity.ServiceAPI), orderRPCRequest()); status.Code(err) != codes.InvalidArgument {
		t.Fatal("unbounded order query accepted", err)
	}
	for _, mutate := range []func(*strategyv1.ListStrategyOrdersRequest){
		func(r *strategyv1.ListStrategyOrdersRequest) { r.StrategyId = 0 },
		func(r *strategyv1.ListStrategyOrdersRequest) { r.Limit = 101 },
		func(r *strategyv1.ListStrategyOrdersRequest) { r.AfterId = -1 },
		func(r *strategyv1.ListStrategyOrdersRequest) { r.State = string(domain.OrderStateFilled) },
		func(r *strategyv1.ListStrategyOrdersRequest) { r.State = "unknown" },
		func(r *strategyv1.ListStrategyOrdersRequest) { r.State = strings.Repeat("x", 2049) },
		func(r *strategyv1.ListStrategyOrdersRequest) { r.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01}) },
	} {
		request := orderRPCRequest()
		mutate(request)
		if _, err := reader.ListStrategyOrders(verified(ctx, rpcsecurity.ServiceAPI), request); status.Code(err) != codes.InvalidArgument {
			t.Fatal("invalid order query reached persistence", err)
		}
	}
	request := orderRPCRequest()
	request.Session.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
	if _, err := reader.ListStrategyOrders(verified(ctx, rpcsecurity.ServiceAPI), request); status.Code(err) != codes.Unauthenticated || store.calls != 0 {
		t.Fatal("invalid proof reached persistence", err)
	}
}

func TestOrderGRPCRejectsWrongScopeCursorMoneyAndPartialSuccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = verified(ctx, rpcsecurity.ServiceAPI)
	store := &orderRPCStore{}
	reader, err := NewOrderQuery(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*domain.AdminOrder){
		func(o *domain.AdminOrder) { o.StrategyID++ },
		func(o *domain.AdminOrder) { o.Core.AccountID = "other-account" },
		func(o *domain.AdminOrder) { o.Price = decimal.New(1, 1_000_000_000) },
		func(o *domain.AdminOrder) { o.Core.FrozenAmount = decimal.NewFromInt(-1) },
		func(o *domain.AdminOrder) { o.Core.FilledQuantity = decimal.NewFromInt(3) },
		func(o *domain.AdminOrder) { o.ConfirmedCoreRevision = 4 },
		func(o *domain.AdminOrder) { o.Core = nil },
		func(o *domain.AdminOrder) { o.ErrorCode = "private-storage-diagnostics" },
	} {
		value := orderRPCFixture()
		mutate(&value)
		store.page = domain.AdminOrderPage{Items: []domain.AdminOrder{value}}
		if page, err := reader.ListStrategyOrders(ctx, orderRPCRequest()); page != nil || status.Code(err) != codes.Unavailable {
			t.Fatal("invalid successful order escaped boundary", err)
		}
	}
	store.page = domain.AdminOrderPage{
		Items:       []domain.AdminOrder{orderRPCFixture()},
		NextAfterID: 1,
	}
	if page, err := reader.ListStrategyOrders(ctx, orderRPCRequest()); page != nil || status.Code(err) != codes.Unavailable {
		t.Fatal("wrong order cursor escaped", err)
	}
	store.err = errors.New("private-storage-diagnostics")
	if page, err := reader.ListStrategyOrders(ctx, orderRPCRequest()); page != nil || status.Code(err) != codes.Unavailable || strings.Contains(err.Error(), "private-storage-diagnostics") {
		t.Fatal("read failure leaked partial success or private cause", err)
	}
	store.err = nil
	request := &strategyv1.GetStrategyOrderRequest{
		Session:    proto.CloneOf(orderRPCRequest().Session),
		StrategyId: orderRPCFixture().StrategyID,
		OrderId:    1,
	}
	if detail, err := reader.GetStrategyOrder(ctx, request); detail != nil || status.Code(err) != codes.Unavailable {
		t.Fatal("detail accepted a different order identity", err)
	}
}
