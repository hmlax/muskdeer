package strategygrpc

import (
	"context"
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

type portfolioRPCStore struct {
	calls  int
	token  string
	filter domain.AdminPortfolioFilter
	value  domain.AdminPortfolio
	err    error
}

func (s *portfolioRPCStore) List(_ context.Context, token string, filter domain.AdminPortfolioFilter) (domain.AdminPortfolioPage, error) {
	s.calls++
	s.token, s.filter = token, filter
	return domain.AdminPortfolioPage{Items: []domain.AdminPortfolio{s.value}}, s.err
}

func (s *portfolioRPCStore) Detail(_ context.Context, token string, _ int64) (domain.AdminPortfolio, error) {
	s.calls++
	s.token = token
	return s.value, s.err
}

func portfolioRPCFixture() domain.AdminPortfolio {
	return domain.AdminPortfolio{
		ID:              9007199254740993,
		ExecutionVenue:  domain.ExecutionVenueSimulator,
		Symbol:          "BTC/USDT",
		AccountEnabled:  true,
		AccountRevision: 9007199254740994,
		Projection: domain.PortfolioSnapshot{
			AccountID:    "portfolio.account",
			InstrumentID: 1,
			BaseAsset:    "BTC",
			QuoteAsset:   "USDT",
			Opening: domain.PortfolioOpening{
				AccountID:    "portfolio.account",
				InstrumentID: 1,
				Quantity:     decimal.NewFromInt(10),
				CashQuote:    decimal.NewFromInt(1000),
				CostQuote:    decimal.NewFromInt(1000),
			},
			Values: domain.PortfolioValues{
				Quantity:         decimal.NewFromInt(10),
				CashQuote:        decimal.NewFromInt(1000),
				CostQuote:        decimal.NewFromInt(1000),
				RealizedPnlQuote: decimal.RequireFromString("-0.123456789012345678901234567890123456"),
			},
			Reconciled: true,
			Revision:   1,
		},
		Source: domain.AdminPortfolioFacts{
			Count:                2,
			LatestSourceSequence: 9007199254740995,
		},
		CoreQuoteBalance: &domain.AdminPortfolioBalance{
			ID:        9007199254740996,
			Asset:     "USDT",
			Available: decimal.RequireFromString("1001.8"),
			Frozen:    decimal.Zero,
			Revision:  3,
			UpdatedAt: time.UnixMilli(1699999999000),
		},
		CreatedAt: time.UnixMilli(1700000000000),
		UpdatedAt: time.UnixMilli(1700000000001),
	}
}

func portfolioRPCRequest() *strategyv1.ListPortfoliosRequest {
	return &strategyv1.ListPortfoliosRequest{
		Session:      &strategyv1.AdminSession{Token: testSessionToken},
		Limit:        1,
		AccountId:    "portfolio.account",
		InstrumentId: 1,
	}
}

func TestPortfolioGRPCSeparatesStoredProjectionSourceAndIndependentCore(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = verified(ctx, rpcsecurity.ServiceAPI)
	store := &portfolioRPCStore{value: portfolioRPCFixture()}
	reader, err := NewPortfolioQuery(store)
	if err != nil {
		t.Fatal(err)
	}
	page, err := reader.ListPortfolios(ctx, portfolioRPCRequest())
	if err != nil || len(page.Portfolios) != 1 || page.Portfolios[0].Projection.RealizedPnlQuote != "-0.123456789012345678901234567890123456" || !page.Portfolios[0].Projection.StoredReconciled || page.Portfolios[0].Source.Count != 2 || page.Portfolios[0].Collected.Count != 0 || page.Portfolios[0].CoreBaseBalance != nil || page.Portfolios[0].CoreQuoteBalance.Available != "1001.8" || store.token != testSessionToken || store.filter.AccountID != "portfolio.account" {
		t.Fatal("portfolio RPC hid stale diagnostics, exact PnL or missing funds", err)
	}
	encoded, err := proto.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	var decoded strategyv1.ListPortfoliosReply
	if err := proto.Unmarshal(encoded, &decoded); err != nil || !proto.Equal(page, &decoded) {
		t.Fatal("typed portfolio failed protobuf roundtrip", err)
	}
	store.value.CoreQuoteBalance = nil
	detail, err := reader.GetPortfolio(ctx, &strategyv1.GetPortfolioRequest{
		Session:     portfolioRPCRequest().Session,
		PortfolioId: store.value.ID,
	})
	if err != nil || detail.Portfolio.CoreQuoteBalance != nil || detail.Portfolio.CoreBaseBalance != nil {
		t.Fatal("missing authoritative balances became fabricated zero funds", err)
	}
}

func TestPortfolioGRPCRejectsWrongPeerUnboundedAndAmbiguousRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	store := &portfolioRPCStore{}
	reader, err := NewPortfolioQuery(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ListPortfolios(verified(ctx, rpcsecurity.ServicePortfolio), portfolioRPCRequest()); status.Code(err) != codes.PermissionDenied {
		t.Fatal("non-API service read management portfolios", err)
	}
	if _, err := reader.ListPortfolios(verified(t.Context(), rpcsecurity.ServiceAPI), portfolioRPCRequest()); status.Code(err) != codes.InvalidArgument {
		t.Fatal("unbounded management read accepted", err)
	}
	for _, change := range []func(*strategyv1.ListPortfoliosRequest){
		func(r *strategyv1.ListPortfoliosRequest) { r.Limit = 101 },
		func(r *strategyv1.ListPortfoliosRequest) { r.AfterId = -1 },
		func(r *strategyv1.ListPortfoliosRequest) { r.AccountId = " portfolio.account" },
		func(r *strategyv1.ListPortfoliosRequest) { r.InstrumentId = -1 },
		func(r *strategyv1.ListPortfoliosRequest) { r.Session = nil },
		func(r *strategyv1.ListPortfoliosRequest) { r.AccountId = strings.Repeat("a", 2049) },
		func(r *strategyv1.ListPortfoliosRequest) { r.ProtoReflect().SetUnknown([]byte{0xf8, 7, 1}) },
	} {
		request := portfolioRPCRequest()
		change(request)
		if _, err := reader.ListPortfolios(verified(ctx, rpcsecurity.ServiceAPI), request); status.Code(err) != codes.InvalidArgument && status.Code(err) != codes.Unauthenticated {
			t.Fatal("invalid portfolio request reached storage", err)
		}
	}
	if store.calls != 0 {
		t.Fatal("rejected request touched portfolio storage")
	}
}

func TestPortfolioGRPCRejectsWrongAccountAndInvalidFinancialSuccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = verified(ctx, rpcsecurity.ServiceAPI)
	store := &portfolioRPCStore{}
	reader, err := NewPortfolioQuery(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*domain.AdminPortfolio){
		func(v *domain.AdminPortfolio) { v.ID = 0 },
		func(v *domain.AdminPortfolio) { v.Projection.AccountID = "another.account" },
		func(v *domain.AdminPortfolio) { v.Projection.InstrumentID++ },
		func(v *domain.AdminPortfolio) { v.Projection.Values.RealizedPnlQuote = decimal.New(1, 2147483647) },
		func(v *domain.AdminPortfolio) { v.Projection.Values.CashQuote = decimal.NewFromInt(-1) },
		func(v *domain.AdminPortfolio) { v.Source.Count = -1 },
		func(v *domain.AdminPortfolio) { v.Collected.Count = 1 },
		func(v *domain.AdminPortfolio) { v.CoreQuoteBalance.Asset = "BTC" },
		func(v *domain.AdminPortfolio) { v.CoreQuoteBalance.Frozen = decimal.NewFromInt(-1) },
	} {
		store.value = portfolioRPCFixture()
		change(&store.value)
		if reply, err := reader.ListPortfolios(ctx, portfolioRPCRequest()); status.Code(err) != codes.Unavailable || reply != nil {
			t.Fatal("invalid or mismatched portfolio success escaped", err)
		}
	}
}
