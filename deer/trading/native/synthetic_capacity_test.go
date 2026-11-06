package native

import (
	"fmt"
	"testing"

	"google.golang.org/protobuf/proto"
)

func TestSyntheticConfigSupportsBoundedMultiMarketRuns(t *testing.T) {
	t.Parallel()
	for _, count := range []int{19, 32, 33} {
		t.Run(fmt.Sprintf("%d markets", count), func(t *testing.T) {
			t.Parallel()
			policy, _ := syntheticFixture()
			market, tape := policy.Profiles[0], policy.SyntheticTape.Profiles[0]
			policy.Profiles, policy.SyntheticTape.Profiles = nil, nil
			for i := range count {
				symbol := fmt.Sprintf("COIN%dUSDT", i)
				profile := proto.CloneOf(market)
				profile.Symbol = symbol
				publication := proto.CloneOf(tape)
				publication.Symbol = symbol
				policy.Profiles = append(policy.Profiles, profile)
				policy.SyntheticTape.Profiles = append(policy.SyntheticTape.Profiles, publication)
			}
			err := ValidateSyntheticConfig(policy)
			if count <= 32 && err != nil {
				t.Fatalf("ValidateSyntheticConfig(%d markets) = %v, want success", count, err)
			}
			if count > 32 && err == nil {
				t.Fatalf("ValidateSyntheticConfig(%d markets) = nil, want capacity rejection", count)
			}
		})
	}
}
