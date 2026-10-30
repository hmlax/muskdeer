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

type tradeRPCStore struct {
	calls  int
	token  string
	filter domain.AdminTradeFilter
	value  domain.AdminTrade
	err    error
}

func (s *tradeRPCStore) List(_ context.Context, token string, filter domain.AdminTradeFilter) (domain.AdminTradePage, error) {
	s.calls++
	s.token, s.filter = token, filter
	return domain.AdminTradePage{Items: []domain.AdminTrade{s.value}}, s.err
}

func (s *tradeRPCStore) Detail(_ context.Context, token string, _, _ int64) (domain.AdminTrade, error) {
	s.calls++
	s.token = token
	return s.value, s.err
}

func tradeRPCFixture() domain.AdminTrade {
	id := int64(9007199254740997)
	return domain.AdminTrade{
		ID:             9007199254740993,
		ExecutionVenue: domain.ExecutionVenueSimulator,
		TradeID:        "trade-query-exact-fee",
		StrategyID:     9007199254740995,
		AccountID:      "account-trades",
		InstrumentID:   1,
		ConfigVersion:  1,
		Source:         domain.MarketSourceBinance,
		CoreOrderID:    9007199254740996,
		ClientOrderID:  "trade-query-client",
		OrderID:        &id,
		Side:           domain.SideBuy,
		Price:          decimal.RequireFromString("99.99"),
		Quantity:       decimal.NewFromInt(1),
		QuoteNotional:  decimal.RequireFromString("99.99"),
		FeeAmount:      decimal.RequireFromString("0.12344444333344436544"),
		FeeAsset:       "USDT",
		RecordedAt:     time.UnixMilli(1700000000000),
		PortfolioFact: &domain.AdminTradePortfolioFact{
			ID:         9007199254740998,
			RecordedAt: time.UnixMilli(1700000000001),
		},
	}
}

func tradeRPCRequest() *strategyv1.ListStrategyTradesRequest {
	return &strategyv1.ListStrategyTradesRequest{
		Session:    &strategyv1.AdminSession{Token: testSessionToken},
		StrategyId: 9007199254740995,
		Limit:      1,
		OrderId:    9007199254740997,
		Side:       string(domain.SideBuy),
	}
}

func TestTradeGRPCPreservesExactFeesOriginalLinkAndCollectionPresence(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = verified(ctx, rpcsecurity.ServiceAPI)
	store := &tradeRPCStore{value: tradeRPCFixture()}
	reader, err := NewTradeQuery(store)
	if err != nil {
		t.Fatal(err)
	}
	page, err := reader.ListStrategyTrades(ctx, tradeRPCRequest())
	if err != nil || len(page.Trades) != 1 || page.Trades[0].FeeAmount != "0.12344444333344436544" || page.Trades[0].GetOrderId() != 9007199254740997 || page.Trades[0].PortfolioFact.Id != 9007199254740998 || store.token != testSessionToken || store.filter.OrderID != 9007199254740997 || store.filter.Side != domain.SideBuy {
		t.Fatal("trade RPC lost original identity, exact fee or collected fact", err)
	}
	encoded, err := proto.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	var decoded strategyv1.ListStrategyTradesReply
	if err := proto.Unmarshal(encoded, &decoded); err != nil || !proto.Equal(page, &decoded) {
		t.Fatal("concrete trade contract failed protobuf roundtrip", err)
	}
	store.value.OrderID, store.value.PortfolioFact = nil, nil
	detail, err := reader.GetStrategyTrade(ctx, &strategyv1.GetStrategyTradeRequest{
		Session:    tradeRPCRequest().Session,
		StrategyId: store.value.StrategyID,
		TradeId:    store.value.ID,
	})
	if err != nil || detail.Trade.OrderId != nil || detail.Trade.PortfolioFact != nil {
		t.Fatal("originally unlinked/uncollected fill acquired a synthetic fact", err)
	}
}

func TestTradeGRPCRequiresVerifiedAPIBoundedKnownRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	store := &tradeRPCStore{}
	reader, err := NewTradeQuery(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ListStrategyTrades(verified(ctx, rpcsecurity.ServicePortfolio), tradeRPCRequest()); status.Code(err) != codes.PermissionDenied {
		t.Fatal("other service read trades", err)
	}
	if _, err := reader.ListStrategyTrades(verified(t.Context(), rpcsecurity.ServiceAPI), tradeRPCRequest()); status.Code(err) != codes.InvalidArgument {
		t.Fatal("unbounded trade request accepted", err)
	}
	for _, change := range []func(*strategyv1.ListStrategyTradesRequest){
		func(r *strategyv1.ListStrategyTradesRequest) { r.StrategyId = 0 },
		func(r *strategyv1.ListStrategyTradesRequest) { r.OrderId = -1 },
		func(r *strategyv1.ListStrategyTradesRequest) { r.AfterId = -1 },
		func(r *strategyv1.ListStrategyTradesRequest) { r.Limit = 101 },
		func(r *strategyv1.ListStrategyTradesRequest) { r.Side = "buy" },
		func(r *strategyv1.ListStrategyTradesRequest) { r.Session = nil },
		func(r *strategyv1.ListStrategyTradesRequest) { r.Side = strings.Repeat("x", 2049) },
		func(r *strategyv1.ListStrategyTradesRequest) { r.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 1}) },
	} {
		request := tradeRPCRequest()
		change(request)
		if _, err := reader.ListStrategyTrades(verified(ctx, rpcsecurity.ServiceAPI), request); status.Code(err) != codes.InvalidArgument && status.Code(err) != codes.Unauthenticated {
			t.Fatal("ambiguous trade request reached storage", err)
		}
	}
	if store.calls != 0 {
		t.Fatal("invalid request touched trade storage")
	}
}

func TestTradeGRPCRejectsMismatchedOrCorruptSuccessWithoutPartialData(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = verified(ctx, rpcsecurity.ServiceAPI)
	store := &tradeRPCStore{value: tradeRPCFixture()}
	reader, err := NewTradeQuery(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*domain.AdminTrade){
		func(v *domain.AdminTrade) { v.StrategyID++ },
		func(v *domain.AdminTrade) { v.ID = 0 },
		func(v *domain.AdminTrade) { v.Source = "other" },
		func(v *domain.AdminTrade) { v.OrderID = nil },
		func(v *domain.AdminTrade) { v.Side = domain.SideSell },
		func(v *domain.AdminTrade) { v.Price = decimal.New(1, 2147483647) },
		func(v *domain.AdminTrade) { v.QuoteNotional = decimal.NewFromInt(100) },
		func(v *domain.AdminTrade) { v.FeeAmount = decimal.NewFromInt(100) },
		func(v *domain.AdminTrade) { v.PortfolioFact.ID = 0 },
	} {
		store.value = tradeRPCFixture()
		change(&store.value)
		if reply, err := reader.ListStrategyTrades(ctx, tradeRPCRequest()); status.Code(err) != codes.Unavailable || reply != nil {
			t.Fatal("malformed trade success escaped as a partial page", err)
		}
	}
	store.value = tradeRPCFixture()
	if reply, err := reader.GetStrategyTrade(ctx, &strategyv1.GetStrategyTradeRequest{
		Session:    tradeRPCRequest().Session,
		StrategyId: store.value.StrategyID,
		TradeId:    store.value.ID + 1,
	}); status.Code(err) != codes.Unavailable || reply != nil {
		t.Fatal("different trade identity escaped detail binding", err)
	}
	store.value, store.err = tradeRPCFixture(), errors.New("private persistence diagnostic")
	if reply, err := reader.ListStrategyTrades(ctx, tradeRPCRequest()); status.Code(err) != codes.Unavailable || reply != nil || strings.Contains(err.Error(), "private") {
		t.Fatal("trade storage failure leaked private details", err)
	}
}
