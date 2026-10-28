package strategygrpc

import (
	"context"
	"errors"
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

type catalogStore struct {
	calls int
	token string
	err   error
	mode  domain.MarketMode
	page  domain.AdminPage
}

func (s *catalogStore) Accounts(_ context.Context, token string, page domain.AdminPage) (domain.AdminAccountPage, error) {
	s.calls++
	s.token, s.page = token, page
	return domain.AdminAccountPage{
		Items: []domain.AdminAccount{{
			ID:           9007199254740993,
			AccountID:    "catalog-account",
			SubjectID:    "catalog-subject",
			Enabled:      false,
			QuoteFeeRate: decimal.RequireFromString("0.123456789012345678"),
			Revision:     1,
		}},
		NextAfterID: 9007199254740993,
	}, s.err
}

func (s *catalogStore) Instruments(_ context.Context, token string, page domain.AdminPage) (domain.AdminInstrumentPage, error) {
	s.calls++
	s.token, s.page = token, page
	return domain.AdminInstrumentPage{Items: []domain.AdminInstrument{{
		Rules: domain.StrategyInstrument{
			ID:              9007199254740993,
			MetadataVersion: 1,
			Status:          domain.InstrumentStatusHalted,
			PriceTick:       decimal.RequireFromString("0.123456789012345678"),
			QuantityStep:    decimal.RequireFromString("0.000000000000000001"),
			MinNotional:     decimal.RequireFromString("99999999999999999999.123456789012345678"),
		},
		ExecutionVenue: domain.ExecutionVenueSimulator,
		Symbol:         "BTC-USDT",
		BaseAsset:      "BTC",
		QuoteAsset:     "USDT",
	}}}, s.err
}

func (s *catalogStore) Feeds(_ context.Context, token string, instrumentID int64, mode domain.MarketMode, page domain.AdminPage) (domain.AdminFeedPage, error) {
	s.calls++
	s.token, s.mode, s.page = token, mode, page
	return domain.AdminFeedPage{Items: []domain.MarketFeedDefinition{{
		ID:            9007199254740993,
		Source:        domain.MarketSourceOKX,
		InstrumentID:  instrumentID,
		Symbol:        "BTC-USDT",
		Mode:          mode,
		SourceVersion: domain.MarketSourceVersion,
		Policy:        marketquality.DefaultPolicy(),
	}}}, s.err
}

func TestCatalogGRPCKeepsExplicitPrecisionModesAndCurrentAuthorityFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = verified(ctx, rpcsecurity.ServiceAPI)
	store := &catalogStore{}
	server, err := NewCatalog(store)
	if err != nil {
		t.Fatal(err)
	}
	session := &strategyv1.AdminSession{Token: testSessionToken}
	accounts, err := server.ListAccounts(ctx, &strategyv1.ListAccountsRequest{
		Session: session,
		AfterId: 9007199254740992,
		Limit:   1,
	})
	if err != nil || accounts.NextAfterId != 9007199254740993 || accounts.Accounts[0].Enabled || accounts.Accounts[0].QuoteFeeRate != "0.123456789012345678" || store.token != testSessionToken || store.page.AfterID != 9007199254740992 {
		t.Fatal("catalog lost exact account identity/fee or authority", err)
	}
	instruments, err := server.ListInstruments(ctx, &strategyv1.ListInstrumentsRequest{
		Session: session,
		Limit:   1,
	})
	if err != nil || instruments.Instruments[0].MinNotional != "99999999999999999999.123456789012345678" || instruments.Instruments[0].QuantityStep != "0.000000000000000001" || instruments.Instruments[0].Status != domain.InstrumentStatusHalted {
		t.Fatal("catalog rounded execution precision or enabled a halted product", err)
	}
	feeds, err := server.ListMarketFeeds(ctx, &strategyv1.ListMarketFeedsRequest{
		Session:      session,
		InstrumentId: 9007199254740993,
		Mode:         string(domain.MarketModeReplay),
		Limit:        1,
	})
	if err != nil || feeds.Feeds[0].Source != string(domain.MarketSourceOKX) || feeds.Feeds[0].InstrumentId != 9007199254740993 || store.mode != domain.MarketModeReplay || feeds.Feeds[0].Policy == nil {
		t.Fatal("catalog substituted source/product/mode or omitted policy", err)
	}
	store.err = errors.New("private-database-detail")
	if _, err := server.ListAccounts(ctx, &strategyv1.ListAccountsRequest{
		Session: session,
		Limit:   1,
	}); status.Code(err) != codes.Unavailable || status.Convert(err).Message() != domain.MessageAdminUnavailable {
		t.Fatal("catalog leaked a private failure", err)
	}
	store.err = domain.ErrUnauthenticated
	if _, err := server.ListInstruments(ctx, &strategyv1.ListInstrumentsRequest{
		Session: session,
		Limit:   1,
	}); status.Code(err) != codes.Unauthenticated {
		t.Fatal("catalog hid current session revocation", err)
	}
}

func TestCatalogGRPCRejectsPeerAmbiguityAndUnboundedInputBeforeStorage(t *testing.T) {
	if _, err := NewCatalog(nil); !errors.Is(err, domain.ErrInvalidAdminQuery) {
		t.Fatal("nil catalog accepted", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	store := &catalogStore{}
	server, err := NewCatalog(store)
	if err != nil {
		t.Fatal(err)
	}
	request := &strategyv1.ListAccountsRequest{
		Session: &strategyv1.AdminSession{Token: testSessionToken},
		Limit:   1,
	}
	for _, peer := range []context.Context{ctx, verified(ctx, rpcsecurity.ServiceExecution), verified(ctx, rpcsecurity.ServiceStrategy)} {
		if _, err := server.ListAccounts(peer, request); status.Code(err) != codes.PermissionDenied {
			t.Fatal("non-API peer reached catalog", err)
		}
	}
	api := verified(ctx, rpcsecurity.ServiceAPI)
	if _, err := server.ListAccounts(verified(context.WithoutCancel(ctx), rpcsecurity.ServiceAPI), request); status.Code(err) != codes.InvalidArgument {
		t.Fatal("unbounded catalog call accepted", err)
	}
	request.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
	if _, err := server.ListAccounts(api, request); status.Code(err) != codes.InvalidArgument {
		t.Fatal("unknown catalog field accepted", err)
	}
	request.ProtoReflect().SetUnknown(nil)
	request.Session.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
	if _, err := server.ListAccounts(api, request); status.Code(err) != codes.Unauthenticated {
		t.Fatal("unknown catalog credential field accepted", err)
	}
	if _, err := server.ListInstruments(api, &strategyv1.ListInstrumentsRequest{Limit: domain.MaxAdminPageSize + 1}); status.Code(err) != codes.InvalidArgument {
		t.Fatal("unbounded instrument page accepted", err)
	}
	if _, err := server.ListMarketFeeds(api, &strategyv1.ListMarketFeedsRequest{
		Session:      &strategyv1.AdminSession{Token: testSessionToken},
		InstrumentId: 1,
		Limit:        1,
	}); status.Code(err) != codes.InvalidArgument || store.calls != 0 {
		t.Fatal("unspecified feed mode reached storage", err)
	}
}
