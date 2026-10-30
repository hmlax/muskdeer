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
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type marketHTTPClient struct {
	strategyHTTPClient
	request *strategyv1.ListMarketsRequest
	mutate  func(*strategyv1.ListMarketsReply)
}

func marketHTTPValue() *strategyv1.MarketMonitor {
	return &strategyv1.MarketMonitor{
		Feed: &strategyv1.CatalogMarketFeed{
			Id:             9007199254740993,
			Source:         string(domain.MarketSourceBinance),
			InstrumentId:   9007199254740994,
			ExternalSymbol: "BTCUSDT",
			Mode:           string(domain.MarketModeReplay),
			SourceVersion:  domain.MarketSourceVersion,
			Policy: &commonv1.MarketPolicy{
				MaximumAgeMs:     2000,
				MaximumSkewMs:    100,
				MaximumSpreadBps: "100",
				MaximumJumpBps:   "100",
				RecoverySamples:  2,
				RecoveryWindowMs: 10,
			},
		},
		Instrument: &strategyv1.CatalogInstrument{
			Id:              9007199254740994,
			ExecutionVenue:  domain.ExecutionVenueSimulator,
			Symbol:          "BTC-USDT",
			BaseAsset:       "BTC",
			QuoteAsset:      "USDT",
			PriceTick:       "0.000000000000000001",
			QuantityStep:    "0.000000000000000001",
			MinNotional:     "99999999999999999999.123456789012345678",
			Status:          domain.InstrumentStatusTrading,
			MetadataVersion: 9007199254740995,
		},
		EvaluatedAtMs: 1700000000000,
	}
}

func (c *marketHTTPClient) ListMarkets(ctx context.Context, request *strategyv1.ListMarketsRequest, _ ...grpc.CallOption) (*strategyv1.ListMarketsReply, error) {
	c.record(ctx, request.Session)
	c.request = request
	reply := &strategyv1.ListMarketsReply{
		Markets:     []*strategyv1.MarketMonitor{marketHTTPValue()},
		NextAfterId: 9007199254740993,
	}
	if c.mutate != nil {
		c.mutate(reply)
	}
	return reply, c.failure
}

func (c *marketHTTPClient) GetMarket(ctx context.Context, request *strategyv1.GetMarketRequest, _ ...grpc.CallOption) (*strategyv1.GetMarketReply, error) {
	page, err := c.ListMarkets(ctx, &strategyv1.ListMarketsRequest{Session: request.Session})
	return &strategyv1.GetMarketReply{Market: page.Markets[0]}, err
}

func marketHTTPFixture(t *testing.T, client strategyv1.StrategyMarketQueryClient) *server.Hertz {
	t.Helper()
	routes, err := New(&authFixture{}, testOrigin, true)
	if err != nil {
		t.Fatal(err)
	}
	routes.markets = client
	hertz := server.New()
	routes.Register(hertz)
	return hertz
}

func TestMarketHTTPPreservesStringsMissingObservationAndExplicitMode(t *testing.T) {
	client := &marketHTTPClient{}
	hertz := marketHTTPFixture(t, client)
	reply := perform(hertz, "GET", "/api/v1/markets?mode=REPLAY&source=binance&instrument_id=9007199254740994&limit=1&after_id=9007199254740992", "", orderHTTPCookie())
	var body struct {
		Markets []struct {
			Feed struct {
				ID string `json:"id"`
			} `json:"feed"`
			Instrument struct {
				MinNotional string `json:"min_notional"`
				Version     string `json:"metadata_version"`
			} `json:"instrument"`
			Snapshot *struct{} `json:"snapshot"`
		} `json:"markets"`
		Next string `json:"next_after_id"`
	}
	if reply.Code != 200 || !client.bounded || client.token != testSecret(1) || string(reply.Header().Peek("Cache-Control")) != "no-store" {
		t.Fatal("market HTTP lost bounded proof or cache policy", reply.Code)
	}
	if err := json.Unmarshal(reply.Body.Bytes(), &body); err != nil || len(body.Markets) != 1 || body.Next != "9007199254740993" || body.Markets[0].Feed.ID != body.Next || body.Markets[0].Snapshot != nil || body.Markets[0].Instrument.Version != "9007199254740995" || body.Markets[0].Instrument.MinNotional != "99999999999999999999.123456789012345678" || client.request.Mode != string(domain.MarketModeReplay) || client.request.Source != string(domain.MarketSourceBinance) || client.request.InstrumentId != 9007199254740994 {
		t.Fatal("market HTTP rounded fields or lost filters/absence", err)
	}
	if reply := perform(hertz, "GET", "/api/v1/markets/9007199254740993", "", orderHTTPCookie()); reply.Code != 200 {
		t.Fatal("market detail did not preserve explicit feed", reply.Code)
	}
}

func TestMarketHTTPRejectsAmbiguityMissingCookieAndWrites(t *testing.T) {
	client := &marketHTTPClient{}
	hertz := marketHTTPFixture(t, client)
	for _, path := range []string{
		"/api/v1/markets", "/api/v1/markets?mode=", "/api/v1/markets?mode=live", "/api/v1/markets?mode=LIVE&mode=REPLAY",
		"/api/v1/markets?mode=REPLAY&source=", "/api/v1/markets?mode=REPLAY&source=bitget", "/api/v1/markets?mode=REPLAY&source=binance&source=okx",
		"/api/v1/markets?mode=REPLAY&instrument_id=0", "/api/v1/markets?mode=REPLAY&instrument_id=01", "/api/v1/markets?mode=REPLAY&instrument_id=1&instrument_id=2",
		"/api/v1/markets?mode=REPLAY&limit=101", "/api/v1/markets?mode=REPLAY&limit=01", "/api/v1/markets?mode=REPLAY&after_id=-1",
		"/api/v1/markets?mode=REPLAY&session=forged", "/api/v1/markets?mode=REPLAY&" + strings.Repeat("x", 1025),
		"/api/v1/markets/01", "/api/v1/markets/0", "/api/v1/markets/9223372036854775808", "/api/v1/markets/1?mode=REPLAY",
	} {
		if reply := perform(hertz, "GET", path, "", orderHTTPCookie()); reply.Code != 400 {
			t.Fatal("ambiguous market query reached RPC", path, reply.Code)
		}
	}
	path := "/api/v1/markets?mode=REPLAY"
	if reply := perform(hertz, "GET", path, "{}", orderHTTPCookie()); reply.Code != 400 {
		t.Fatal("market GET body reached RPC", reply.Code)
	}
	if reply := perform(hertz, "GET", path, ""); reply.Code != 401 {
		t.Fatal("market query accepted missing cookie", reply.Code)
	}
	if reply := perform(hertz, "GET", path, "", orderHTTPCookie(), orderHTTPCookie()); reply.Code != 401 {
		t.Fatal("market query accepted duplicate cookie", reply.Code)
	}
	if reply := perform(hertz, "POST", "/api/v1/markets", "{}", orderHTTPCookie()); reply.Code != 405 || client.calls != 0 {
		t.Fatal("market query exposed a write or called invalid request", reply.Code)
	}
	if reply := perform(marketHTTPFixture(t, nil), "GET", path, "", orderHTTPCookie()); reply.Code != 404 {
		t.Fatal("disabled management query exposed markets", reply.Code)
	}
}

func TestMarketHTTPRejectsForeignSuccessAndSanitizesDependencyFailure(t *testing.T) {
	client := &marketHTTPClient{}
	hertz := marketHTTPFixture(t, client)
	path := "/api/v1/markets?mode=REPLAY&limit=1"
	for _, mutate := range []func(*strategyv1.ListMarketsReply){
		func(p *strategyv1.ListMarketsReply) { p.Markets[0].Feed.Mode = string(domain.MarketModeLive) },
		func(p *strategyv1.ListMarketsReply) { p.Markets[0].Instrument.Id++ },
		func(p *strategyv1.ListMarketsReply) { p.Markets[0].Instrument.MinNotional = "NaN" },
		func(p *strategyv1.ListMarketsReply) { p.NextAfterId++ },
		func(p *strategyv1.ListMarketsReply) { p.Markets = append(p.Markets, p.Markets[0]) },
		func(p *strategyv1.ListMarketsReply) {
			p.Markets[0].Feed.Policy.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
		},
		func(p *strategyv1.ListMarketsReply) { p.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01}) },
	} {
		client.mutate = mutate
		if reply := perform(hertz, "GET", path, "", orderHTTPCookie()); reply.Code != 503 || strings.Contains(reply.Body.String(), "external_symbol") {
			t.Fatal("invalid successful market response escaped", reply.Code)
		}
	}
	client.mutate = nil
	if reply := perform(hertz, "GET", "/api/v1/markets/1", "", orderHTTPCookie()); reply.Code != 503 {
		t.Fatal("market detail accepted a different identity", reply.Code)
	}
	for _, code := range []codes.Code{codes.Unauthenticated, codes.PermissionDenied, codes.NotFound, codes.Unavailable} {
		client.failure = status.Error(code, "private-market-storage-details")
		want := map[codes.Code]int{codes.Unauthenticated: 401, codes.PermissionDenied: 403, codes.NotFound: 404, codes.Unavailable: 503}[code]
		if reply := perform(hertz, "GET", path, "", orderHTTPCookie()); reply.Code != want || strings.Contains(reply.Body.String(), "private-market-storage-details") || strings.Contains(reply.Body.String(), "external_symbol") {
			t.Fatal("market failure leaked partial response or private cause", reply.Code)
		}
	}
}
