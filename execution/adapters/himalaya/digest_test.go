package himalaya

import (
	"encoding/hex"
	"testing"
)

func TestGatewayWireDigestsMatchSDKRequests(t *testing.T) {
	t.Parallel()
	// Canonical deterministic encodings of the actual SDK place/cancel requests.
	const expectedPlace = "c601abe3bb4d6f180c2ade0cb68a107c51b2529833b4121edc22d43510d3e460"
	const expectedCancel = "52a95af5f4034f4f3b2909bd99c8d033d015edddb23e343251af71f1ca7e2385"
	place, err := (Factory{}).RequestDigest(sdkOrder())
	if err != nil || hex.EncodeToString(place[:]) != expectedPlace {
		t.Fatalf("venue place digest = %x, %v; want canonical SDK request identity", place, err)
	}
	cancel, err := (Factory{}).CancelDigest(CancelRequest{
		Scope: Scope{
			UserID: 10001,
			FundID: 10001,
			Symbol: "BTCUSDT",
		},
		OrderID: 42,
	})
	if err != nil || hex.EncodeToString(cancel[:]) != expectedCancel {
		t.Fatalf("venue cancel digest = %x, %v; want canonical SDK request identity", cancel, err)
	}
}
