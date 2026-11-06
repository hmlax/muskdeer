package runtimeconfig

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func TestDatabaseVenueParametersRequireExplicitBoundedMaintenance(t *testing.T) {
	t.Parallel()
	parameters := &configv1.BusinessRuntime{
		Enabled:          true,
		ExecutionVenue:   native.Venue,
		PollInterval:     durationpb.New(200 * time.Millisecond),
		OperationTimeout: durationpb.New(time.Second),
		Venue: &configv1.VenueRuntime{
			QuantityFraction:    "0.123456789012345678",
			ReplacementFraction: "0.25",
			Depth:               20,
			BridgeSlots:         2,
			LeaseTtl:            durationpb.New(10 * time.Second),
		},
	}
	for _, service := range []string{"market", "strategy", "execution", "portfolio"} {
		if err := ValidatePolicyParameters(service, parameters); err != nil {
			t.Fatalf("valid database parameters unexpectedly require transport fields: %v", err)
		}
	}
	for _, mutate := range []func(*configv1.BusinessRuntime){
		func(p *configv1.BusinessRuntime) { p.Venue.ReplacementFraction = "" },
		func(p *configv1.BusinessRuntime) { p.Venue.ReplacementFraction = "0" },
		func(p *configv1.BusinessRuntime) { p.Venue.ReplacementFraction = "1.000000000000000001" },
		func(p *configv1.BusinessRuntime) { p.Venue.BridgeSlots = 21 },
		func(p *configv1.BusinessRuntime) { p.OperationTimeout = nil },
		func(p *configv1.BusinessRuntime) { p.Venue.LeaseTtl = durationpb.New(2 * time.Second) },
		func(p *configv1.BusinessRuntime) {
			p.ExecutionVenue = "himalaya_sdk"
			p.Enabled = false
		},
	} {
		changed := proto.CloneOf(parameters)
		mutate(changed)
		if ValidatePolicyParameters("execution", changed) == nil {
			t.Fatal("missing or unbounded execution parameters accepted")
		}
	}
}
