package native

import (
	"math"
	"testing"

	"google.golang.org/protobuf/proto"
)

func TestSyntheticZeroLimitsPermitContinuedPublication(t *testing.T) {
	h, book := syntheticFixture()
	h.SyntheticTape.MaximumEvents = 0
	h.SyntheticTape.Profiles[0].MaximumNotional = "0"
	if err := ValidateSyntheticConfig(h); err != nil {
		t.Fatalf("ValidateSyntheticConfig(zero limits) = %v, want unlimited development run", err)
	}
	if _, err := SyntheticPlan(h, book.Symbol, 1001, book, nil); err != nil {
		t.Fatalf("SyntheticPlan(sequence 1001, zero limits) = %v, want continued publication", err)
	}
	if _, err := SyntheticPlan(h, book.Symbol, uint64(math.MaxUint32)+1, book, nil); err != nil {
		t.Fatalf("SyntheticPlan(sequence beyond uint32) = %v, want monotonic 64-bit continuation", err)
	}
	if _, err := SyntheticPlan(h, book.Symbol, uint64(math.MaxInt64)+1, book, nil); err == nil {
		t.Fatal("SyntheticPlan(sequence outside signed storage range) accepted overflow")
	}
	finite := proto.CloneOf(h)
	finite.SyntheticTape.MaximumEvents = 1001
	if _, err := SyntheticPlan(finite, book.Symbol, 1001, book, nil); err == nil {
		t.Fatal("SyntheticPlan(sequence 1001, explicit limit 1001) accepted exhausted run")
	}
	finite.SyntheticTape.MaximumEvents = 0
	finite.SyntheticTape.Profiles[0].MaximumNotional = "0.5"
	if _, err := SyntheticPlan(finite, book.Symbol, 1001, book, nil); err == nil {
		t.Fatal("SyntheticPlan(explicit positive notional limit) ignored limit")
	}
}
