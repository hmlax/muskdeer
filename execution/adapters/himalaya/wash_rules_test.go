package himalaya

import (
	"errors"
	"sync"
	"testing"
	"time"

	pb "gitlab.com/himalaya/lukla-go/pb"

	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

func pepeWashMarket() *pb.MarketView {
	return &pb.MarketView{
		Symbol:                  "1000PEPEUSDT",
		Status:                  washMarketActive,
		EffectiveBasePrecision:  0,
		EffectiveQuotePrecision: 7,
		PriceTickTicks:          1,
		BaseIncrement:           1,
		BaseMinSize:             1,
		BaseMaxSize:             1000000,
		QuoteMinSize:            50000000,
	}
}

func TestWashMinimumFiltersReportedNotionalBeforeSDKAndAllowsExactBoundary(t *testing.T) {
	peer := &sdkPeer{washMarket: pepeWashMarket()}
	client := sdkClient(t, peer, nil)
	input := &gateway.SyntheticRequest{
		Symbol:   "1000PEPEUSDT",
		Side:     domain.SideBuy,
		Price:    "0.3131492",
		Quantity: "1",
	}
	if _, err := client.WashTrade(t.Context(), input); !errors.Is(err, gateway.ErrBelowMarketMinimum) || !errors.Is(err, gateway.ErrNotDispatched) {
		t.Fatalf("under-minimum request was sent: %v", err)
	}
	peer.mu.Lock()
	calls := len(peer.syntheticRequests)
	peer.mu.Unlock()
	if calls != 0 {
		t.Fatal("reported 31314920 quote-minor notional reached SDK")
	}
	input.Price = "0.5"
	if _, err := client.WashTrade(t.Context(), input); err != nil {
		t.Fatalf("exact 50000000 quote-minor boundary rejected: %v", err)
	}
	input.Price = "0.4999999"
	if _, err := client.WashTrade(t.Context(), input); !errors.Is(err, gateway.ErrBelowMarketMinimum) {
		t.Fatalf("one quote tick below minimum accepted: %v", err)
	}
	peer.mu.Lock()
	defer peer.mu.Unlock()
	if len(peer.syntheticRequests) != 1 || peer.syntheticRequests[0].Price != "0.5" || peer.syntheticRequests[0].Quantity != "1" || peer.marketCalls != 1 {
		t.Fatal("filter changed quantity, retried a write or fetched rules per event")
	}
}

func TestWashRulesCoalesceConcurrentColdReadsAndRejectBadGrids(t *testing.T) {
	peer := &sdkPeer{washMarket: pepeWashMarket()}
	client := sdkClient(t, peer, nil)
	var group sync.WaitGroup
	errorsSeen := make(chan error, 20)
	for range 20 {
		group.Go(func() {
			_, err := client.WashTrade(t.Context(), &gateway.SyntheticRequest{
				Symbol:   "1000PEPEUSDT",
				Side:     domain.SideSell,
				Price:    "0.3131492",
				Quantity: "1",
			})
			errorsSeen <- err
		})
	}
	group.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if !errors.Is(err, gateway.ErrBelowMarketMinimum) {
			t.Fatalf("unexpected filter result: %v", err)
		}
	}
	peer.mu.Lock()
	if peer.marketCalls != 1 || len(peer.syntheticRequests) != 0 {
		t.Fatal("concurrent metadata requests were not coalesced")
	}
	peer.mu.Unlock()
	for _, test := range []struct {
		price    string
		quantity string
	}{
		{
			price:    "0.50000001",
			quantity: "1",
		},
		{
			price:    "0.5",
			quantity: "1.1",
		},
		{
			price:    "0.5",
			quantity: "1000001",
		},
	} {
		_, err := client.WashTrade(t.Context(), &gateway.SyntheticRequest{
			Symbol:   "1000PEPEUSDT",
			Side:     domain.SideBuy,
			Price:    test.price,
			Quantity: test.quantity,
		})
		if !errors.Is(err, gateway.ErrNotDispatched) {
			t.Fatalf("bad grid or maximum reached SDK: %v", err)
		}
	}
}

func TestWashRulesRefreshExpiredEntriesAndDoNotCacheInvalidMarkets(t *testing.T) {
	peer := &sdkPeer{washMarket: pepeWashMarket()}
	client := sdkClient(t, peer, nil)
	input := &gateway.SyntheticRequest{
		Symbol:   "1000PEPEUSDT",
		Side:     domain.SideBuy,
		Price:    "0.5",
		Quantity: "1",
	}
	if _, err := client.WashTrade(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	cached, _ := client.washMarkets.Load(input.Symbol)
	expired := *cached.(*washMarketRules)
	expired.validUntil = time.Now().Add(-time.Second)
	client.washMarkets.Store(input.Symbol, &expired)
	peer.mu.Lock()
	peer.washMarket.QuoteMinSize = 60000000
	peer.mu.Unlock()
	if _, err := client.WashTrade(t.Context(), input); !errors.Is(err, gateway.ErrBelowMarketMinimum) {
		t.Fatalf("expired minimum was used: %v", err)
	}
	client.washMarkets.Delete(input.Symbol)
	peer.mu.Lock()
	peer.washMarket.Status = 0
	peer.mu.Unlock()
	if _, err := client.WashTrade(t.Context(), input); !errors.Is(err, gateway.ErrNotDispatched) {
		t.Fatalf("inactive market reached SDK: %v", err)
	}
	peer.mu.Lock()
	peer.washMarket.Status = washMarketActive
	peer.washMarket.BaseMinSize = 2
	peer.mu.Unlock()
	input.Price = "1"
	if _, err := client.WashTrade(t.Context(), input); !errors.Is(err, gateway.ErrBelowMarketMinimum) {
		t.Fatalf("invalid market was cached or minimum quantity missed: %v", err)
	}
	peer.mu.Lock()
	defer peer.mu.Unlock()
	if peer.marketCalls != 4 || len(peer.syntheticRequests) != 1 {
		t.Fatal("expired or invalid metadata admitted a write")
	}
}
