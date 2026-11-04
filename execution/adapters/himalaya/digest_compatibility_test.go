package himalaya

import (
	"encoding/hex"
	"testing"
)

func TestGatewayWireDigestsMatchPersistedLegacyIdentities(t *testing.T) {
	t.Parallel()
	// Captured from the pre-extraction requestDigest/cancelInput implementation.
	const legacyPlace = "c601abe3bb4d6f180c2ade0cb68a107c51b2529833b4121edc22d43510d3e460"
	const legacyCancel = "52a95af5f4034f4f3b2909bd99c8d033d015edddb23e343251af71f1ca7e2385"
	place, err := (Factory{}).RequestDigest(sdkOrder())
	if err != nil || hex.EncodeToString(place[:]) != legacyPlace {
		t.Fatalf("venue place digest = %x, %v; want persisted legacy identity", place, err)
	}
	cancel, err := (Factory{}).CancelDigest(CancelRequest{
		Scope: Scope{
			UserID: 10001,
			FundID: 10001,
			Symbol: "BTCUSDT",
		},
		OrderID: 42,
	})
	if err != nil || hex.EncodeToString(cancel[:]) != legacyCancel {
		t.Fatalf("venue cancel digest = %x, %v; want persisted legacy identity", cancel, err)
	}
}
