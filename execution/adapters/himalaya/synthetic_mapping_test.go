package himalaya

import (
	"errors"
	"testing"

	pb "gitlab.com/himalaya/lukla-go/pb"

	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

func TestSyntheticAdapterMapsBusinessSideAndRejectsUnknownSide(t *testing.T) {
	t.Parallel()
	peer := &sdkPeer{}
	client := sdkClient(t, peer, &testAdmission{})
	for _, side := range []domain.Side{domain.SideBuy, domain.SideSell} {
		result, err := client.PublishSynthetic(t.Context(), &gateway.SyntheticRequest{
			Symbol:   "BTCUSDT",
			Side:     side,
			Price:    "100.1",
			Quantity: "0.001",
		})
		if err != nil || result.GetTradeID() != "adapter-tape-test" || result.Price != "100.1" || result.Quantity != "0.001" || result.GetTimestampMS() != 1 {
			t.Fatalf("synthetic adapter result = %v, %v; want typed receipt", result, err)
		}
	}
	if _, err := client.PublishSynthetic(t.Context(), &gateway.SyntheticRequest{
		Symbol:   "BTCUSDT",
		Side:     domain.Side("invalid"),
		Price:    "100.1",
		Quantity: "0.001",
	}); !errors.Is(err, ErrInvalidSubmit) {
		t.Fatalf("unknown synthetic side = %v, want rejected", err)
	}
	peer.mu.Lock()
	defer peer.mu.Unlock()
	if len(peer.syntheticRequests) != 2 || peer.syntheticRequests[0].Side != int32(pb.Side_SIDE_BUY) || peer.syntheticRequests[1].Side != int32(pb.Side_SIDE_SELL) {
		t.Fatalf("SDK synthetic requests = %v; want exactly BUY and SELL", peer.syntheticRequests)
	}
}

func TestDirectWashAdapterWorksWithoutPersistenceAdmission(t *testing.T) {
	t.Parallel()
	peer := &sdkPeer{}
	client := sdkClient(t, peer, nil)
	result, err := client.WashTrade(t.Context(), &gateway.SyntheticRequest{
		Symbol:   "BTCUSDT",
		Side:     domain.SideBuy,
		Price:    "100.1",
		Quantity: "0.001",
	})
	if err != nil || result.GetTradeID() != "adapter-tape-test" {
		t.Fatalf("direct SDK = %v, %v", result, err)
	}
	peer.mu.Lock()
	defer peer.mu.Unlock()
	if len(peer.syntheticRequests) != 1 {
		t.Fatalf("SDK called %d times", len(peer.syntheticRequests))
	}
}
