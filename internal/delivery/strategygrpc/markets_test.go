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

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketquality"
)

type marketRPCStore struct {
	calls  int
	token  string
	filter domain.AdminMarketFilter
	page   domain.AdminMarketPage
	err    error
}

func (s *marketRPCStore) List(_ context.Context, token string, filter domain.AdminMarketFilter) (domain.AdminMarketPage, error) {
	s.calls++
	s.token, s.filter = token, filter
	return s.page, s.err
}

func (s *marketRPCStore) Detail(_ context.Context, token string, _ int64) (domain.AdminMarket, error) {
	s.calls++
	s.token = token
	if s.err != nil {
		return domain.AdminMarket{}, s.err
	}
	return s.page.Items[0], nil
}

func marketRPCFixture() domain.AdminMarket {
	return domain.AdminMarket{
		Feed: domain.MarketFeedDefinition{
			ID:            9007199254740993,
			Source:        domain.MarketSourceBinance,
			InstrumentID:  9007199254740994,
			Symbol:        "BTCUSDT",
			Mode:          domain.MarketModeReplay,
			SourceVersion: domain.MarketSourceVersion,
			Policy:        marketquality.DefaultPolicy(),
		},
		Instrument: domain.AdminInstrument{
			Rules: domain.StrategyInstrument{
				ID:              9007199254740994,
				MetadataVersion: 1,
				Status:          domain.InstrumentStatusTrading,
				PriceTick:       decimal.RequireFromString("0.000000000000000001"),
				QuantityStep:    decimal.RequireFromString("0.000000000000000001"),
				MinNotional:     decimal.RequireFromString("99999999999999999999.123456789012345678"),
			},
			ExecutionVenue: domain.ExecutionVenueSimulator,
			Symbol:         "BTC-USDT",
			BaseAsset:      "BTC",
			QuoteAsset:     "USDT",
		},
		EvaluatedAt: time.UnixMilli(1700000000000),
	}
}

func marketRPCRequest() *strategyv1.ListMarketsRequest {
	return &strategyv1.ListMarketsRequest{
		Session:      &strategyv1.AdminSession{Token: testSessionToken},
		Source:       string(domain.MarketSourceBinance),
		InstrumentId: 9007199254740994,
		Mode:         string(domain.MarketModeReplay),
		Limit:        1,
	}
}

func TestMarketGRPCPreservesExactMetadataAndMissingObservation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = verified(ctx, rpcsecurity.ServiceAPI)
	store := &marketRPCStore{page: domain.AdminMarketPage{Items: []domain.AdminMarket{marketRPCFixture()}}}
	reader, err := NewMarketQuery(store)
	if err != nil {
		t.Fatal(err)
	}
	page, err := reader.ListMarkets(ctx, marketRPCRequest())
	if err != nil || len(page.Markets) != 1 || page.Markets[0].Feed.Id != 9007199254740993 || page.Markets[0].Snapshot != nil || page.Markets[0].Instrument.MinNotional != "99999999999999999999.123456789012345678" || store.token != testSessionToken || store.filter.Mode != domain.MarketModeReplay || store.filter.Source != domain.MarketSourceBinance {
		t.Fatal("market RPC lost filter, exact metadata or absence", err)
	}
	detail, err := reader.GetMarket(ctx, &strategyv1.GetMarketRequest{
		Session: marketRPCRequest().Session,
		FeedId:  9007199254740993,
	})
	if err != nil || detail.Market.Snapshot != nil || detail.Market.Feed.Id != page.Markets[0].Feed.Id {
		t.Fatal("market detail fabricated publication or changed identity", err)
	}
}

func TestMarketGRPCRejectsUnverifiedUnboundedAndAmbiguousRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	store := &marketRPCStore{}
	reader, err := NewMarketQuery(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ListMarkets(verified(ctx, rpcsecurity.ServicePortfolio), marketRPCRequest()); status.Code(err) != codes.PermissionDenied {
		t.Fatal("non-API certificate queried management markets", err)
	}
	if _, err := reader.ListMarkets(verified(t.Context(), rpcsecurity.ServiceAPI), marketRPCRequest()); status.Code(err) != codes.InvalidArgument {
		t.Fatal("unbounded management market query accepted", err)
	}
	for _, mutate := range []func(*strategyv1.ListMarketsRequest){
		func(r *strategyv1.ListMarketsRequest) { r.Mode = "" },
		func(r *strategyv1.ListMarketsRequest) { r.Mode = "replay" },
		func(r *strategyv1.ListMarketsRequest) { r.Source = string(domain.MarketSourceBitget) },
		func(r *strategyv1.ListMarketsRequest) { r.InstrumentId = -1 },
		func(r *strategyv1.ListMarketsRequest) { r.AfterId = -1 },
		func(r *strategyv1.ListMarketsRequest) { r.Limit = 101 },
		func(r *strategyv1.ListMarketsRequest) { r.Source = strings.Repeat("x", 2049) },
		func(r *strategyv1.ListMarketsRequest) { r.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01}) },
	} {
		r := marketRPCRequest()
		mutate(r)
		if _, err := reader.ListMarkets(verified(ctx, rpcsecurity.ServiceAPI), r); status.Code(err) != codes.InvalidArgument {
			t.Fatal("ambiguous market query reached storage", err)
		}
	}
	r := marketRPCRequest()
	r.Session.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
	if _, err := reader.ListMarkets(verified(ctx, rpcsecurity.ServiceAPI), r); status.Code(err) != codes.Unauthenticated || store.calls != 0 {
		t.Fatal("invalid current-session proof reached storage", err)
	}
}

func TestMarketGRPCRejectsForeignScopesWrongCursorAndPartialFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = verified(ctx, rpcsecurity.ServiceAPI)
	store := &marketRPCStore{}
	reader, err := NewMarketQuery(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*domain.AdminMarket){
		func(m *domain.AdminMarket) { m.Feed.Mode = domain.MarketModeLive },
		func(m *domain.AdminMarket) { m.Instrument.Rules.ID++ },
		func(m *domain.AdminMarket) { m.Feed.Symbol = "ETHUSDT" },
		func(m *domain.AdminMarket) { m.Instrument.Rules.PriceTick = decimal.New(1, 1_000_000_000) },
	} {
		value := marketRPCFixture()
		mutate(&value)
		store.page = domain.AdminMarketPage{Items: []domain.AdminMarket{value}}
		if reply, err := reader.ListMarkets(ctx, marketRPCRequest()); reply != nil || status.Code(err) != codes.Unavailable {
			t.Fatal("invalid successful market observation escaped", err)
		}
	}
	store.page = domain.AdminMarketPage{
		Items:       []domain.AdminMarket{marketRPCFixture()},
		NextAfterID: 1,
	}
	if reply, err := reader.ListMarkets(ctx, marketRPCRequest()); reply != nil || status.Code(err) != codes.Unavailable {
		t.Fatal("incorrect market cursor escaped", err)
	}
	store.err = errors.New("private-market-storage-details")
	if reply, err := reader.ListMarkets(ctx, marketRPCRequest()); reply != nil || status.Code(err) != codes.Unavailable || strings.Contains(err.Error(), "private-market-storage-details") {
		t.Fatal("market failure exposed partial data or storage diagnostics", err)
	}
	store.err = nil
	if reply, err := reader.GetMarket(ctx, &strategyv1.GetMarketRequest{
		Session: marketRPCRequest().Session,
		FeedId:  1,
	}); reply != nil || status.Code(err) != codes.Unavailable {
		t.Fatal("market detail returned a different feed", err)
	}
}
