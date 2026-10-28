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

type catalogHTTPClient struct{ strategyHTTPClient }

func (c *catalogHTTPClient) ListAccounts(ctx context.Context, request *strategyv1.ListAccountsRequest, _ ...grpc.CallOption) (*strategyv1.ListAccountsReply, error) {
	c.record(ctx, request.Session)
	return &strategyv1.ListAccountsReply{
		Accounts: []*strategyv1.CatalogAccount{{
			Id:           9007199254740993,
			AccountId:    "catalog-account",
			QuoteFeeRate: "0.123456789012345678",
		}},
		NextAfterId: request.AfterId,
	}, c.failure
}

func (c *catalogHTTPClient) ListInstruments(ctx context.Context, request *strategyv1.ListInstrumentsRequest, _ ...grpc.CallOption) (*strategyv1.ListInstrumentsReply, error) {
	c.record(ctx, request.Session)
	return &strategyv1.ListInstrumentsReply{Instruments: []*strategyv1.CatalogInstrument{{
		Id:          9007199254740993,
		MinNotional: "99999999999999999999.123456789012345678",
	}}}, c.failure
}

func (c *catalogHTTPClient) ListMarketFeeds(ctx context.Context, request *strategyv1.ListMarketFeedsRequest, _ ...grpc.CallOption) (*strategyv1.ListMarketFeedsReply, error) {
	c.record(ctx, request.Session)
	return &strategyv1.ListMarketFeedsReply{Feeds: []*strategyv1.CatalogMarketFeed{{
		Id:           9007199254740993,
		InstrumentId: request.InstrumentId,
		Mode:         request.Mode,
		Source:       string(domain.MarketSourceOKX),
	}}}, c.failure
}

func catalogHTTPFixture(t *testing.T, client strategyv1.StrategyCatalogClient) *server.Hertz {
	t.Helper()
	routes, err := New(&authFixture{}, testOrigin, true)
	if err != nil {
		t.Fatal(err)
	}
	routes.catalog = client
	hertz := server.New()
	routes.Register(hertz)
	return hertz
}

func TestCatalogHTTPUsesActualCookieTypedStringsAndNativeBudget(t *testing.T) {
	client := &catalogHTTPClient{}
	hertz := catalogHTTPFixture(t, client)
	cookie := ut.Header{
		Key:   "Cookie",
		Value: secureSessionCookie + "=" + testSecret(1),
	}
	for _, path := range []string{
		"/api/v1/accounts?after_id=9007199254740993&limit=1",
		"/api/v1/instruments?limit=1",
		"/api/v1/instruments/9007199254740993/market-feeds?mode=REPLAY&limit=1",
	} {
		reply := perform(hertz, "GET", path, "", cookie)
		if reply.Code != 200 || !client.bounded || client.token != testSecret(1) || strings.Contains(reply.Body.String(), testSecret(1)) || string(reply.Header().Peek("Cache-Control")) != "no-store" {
			t.Fatal("catalog lost actual cookie/budget or leaked/cacheable data")
		}
		if strings.Contains(path, "/accounts") {
			var result struct {
				Accounts []struct {
					ID           string `json:"id"`
					QuoteFeeRate string `json:"quote_fee_rate"`
				} `json:"accounts"`
				NextAfterID string `json:"next_after_id"`
			}
			if err := json.Unmarshal(reply.Body.Bytes(), &result); err != nil || len(result.Accounts) != 1 || result.Accounts[0].ID != "9007199254740993" || result.Accounts[0].QuoteFeeRate != "0.123456789012345678" || result.NextAfterID != "9007199254740993" {
				t.Fatal("catalog rounded HTTP account/fee/cursor", err)
			}
		}
	}
	client.failure = status.Error(codes.Unauthenticated, "private-detail")
	if reply := perform(hertz, "GET", "/api/v1/accounts", "", cookie); reply.Code != 401 || strings.Contains(reply.Body.String(), "private-detail") {
		t.Fatal("catalog hid session revocation or exposed private error")
	}
}

func TestCatalogHTTPRejectsAmbiguousFiltersAndMissingAuthorityBeforeRPC(t *testing.T) {
	client := &catalogHTTPClient{}
	hertz := catalogHTTPFixture(t, client)
	cookie := ut.Header{
		Key:   "Cookie",
		Value: secureSessionCookie + "=" + testSecret(1),
	}
	for _, path := range []string{
		"/api/v1/accounts?limit=0",
		"/api/v1/accounts?limit=1&limit=2",
		"/api/v1/accounts?session=forged",
		"/api/v1/instruments?after_id=01",
		"/api/v1/instruments?after_id=9223372036854775808",
		"/api/v1/instruments/01/market-feeds?mode=REPLAY",
		"/api/v1/instruments/0/market-feeds?mode=REPLAY",
		"/api/v1/instruments/1/market-feeds",
		"/api/v1/instruments/1/market-feeds?mode=LIVE&mode=REPLAY",
		"/api/v1/instruments/1/market-feeds?mode=live",
		"/api/v1/instruments/1/market-feeds?mode=REPLAY&source=bybit",
	} {
		if reply := perform(hertz, "GET", path, "", cookie); reply.Code != 400 {
			t.Fatal("ambiguous catalog filter accepted", path, reply.Code)
		}
	}
	for _, path := range []string{"/api/v1/accounts", "/api/v1/instruments", "/api/v1/instruments/1/market-feeds?mode=LIVE"} {
		if reply := perform(hertz, "GET", path, ""); reply.Code != 401 {
			t.Fatal("catalog accepted missing cookie", reply.Code)
		}
		if reply := perform(hertz, "POST", path, "", cookie); reply.Code != 405 {
			t.Fatal("catalog exposed a write route", reply.Code)
		}
	}
	if client.calls != 0 {
		t.Fatal("invalid catalog boundary reached RPC")
	}
}
