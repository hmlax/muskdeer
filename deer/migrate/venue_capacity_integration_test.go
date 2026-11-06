//go:build integration

package migrate

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	commonmodelv1 "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
	venueexecution "github.com/hmlax/muskdeer/execution/venue"
)

func TestVenueMultiMarketImportAndStartupPreserveEveryProfile(t *testing.T) {
	db, store, lease, _ := sdkAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := db.WithContext(ctx).Model(&commonmodelv1.ExecutionShardLease{}).Where("account_id = ?", lease.Key.AccountID).UpdateColumn("expires_at", 0).Error; err != nil {
		t.Fatal(err)
	}
	batch := &commonv1.VenueProfileImport{}
	cfg := &configv1.BusinessRuntime{
		PollInterval:     durationpb.New(100 * time.Millisecond),
		OperationTimeout: durationpb.New(time.Second),
		Venue: &configv1.VenueRuntime{
			QuantityFraction:    "0.1",
			ReplacementFraction: "0.1",
			Depth:               20,
			LeaseTtl:            durationpb.New(10 * time.Second),
			SyntheticTape: &configv1.SyntheticTape{
				Enabled:         true,
				DevelopmentOnly: true,
				RunId:           "capacity-test",
				Interval:        durationpb.New(time.Second),
				MaximumEvents:   2,
			},
		},
	}
	for i := range 257 {
		profile := databaseProfileFixture()
		profile.UserId += int64(i)
		profile.FundId += int64(i)
		if i > 0 {
			profile.Symbol = fmt.Sprintf("COIN%dUSDT", i)
		}
		batch.Profiles = append(batch.Profiles, profile)
		cfg.Venue.SyntheticTape.Profiles = append(cfg.Venue.SyntheticTape.Profiles, &configv1.SyntheticTapeProfile{
			Symbol:          profile.Symbol,
			Quantity:        "0.01",
			MaximumNotional: "100",
		})
	}
	if err := store.ImportProfiles(ctx, batch); err != nil {
		t.Fatalf("ImportProfiles(257 markets) = %v, want success", err)
	}
	var before []*commonmodelv1.TradingAccountPolicy
	if err := db.WithContext(ctx).Model(&commonmodelv1.TradingAccountPolicy{}).Order("id").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	settings, err := venueconfig.LoadRuntime(ctx, db, cfg, nil)
	if err != nil {
		t.Fatalf("LoadRuntime(257 markets) = %v, want success", err)
	}
	if len(settings.Venue.Profiles) != len(batch.Profiles) {
		t.Fatalf("LoadRuntime(257 markets) = %d profiles, want 257", len(settings.Venue.Profiles))
	}
	for i, profile := range settings.Venue.Profiles {
		if !proto.Equal(profile, batch.Profiles[i]) {
			t.Errorf("LoadRuntime(profile %d) = %v, want %v", i, profile, batch.Profiles[i])
		}
		if i > 0 && before[i].Enabled {
			t.Errorf("ImportProfiles(new market %s) enabled its budget, want disabled", profile.Symbol)
		}
	}
	invalid := proto.CloneOf(batch)
	invalid.Profiles[0].PriceTick = "0.2"
	invalid.Profiles[len(invalid.Profiles)-1].QuantityStep = "0"
	if err := store.ImportProfiles(ctx, invalid); !errors.Is(err, venueexecution.ErrInvalidSubmit) {
		t.Fatalf("ImportProfiles(invalid final market) = %v, want ErrInvalidSubmit", err)
	}
	var after []*commonmodelv1.TradingAccountPolicy
	if err := db.WithContext(ctx).Model(&commonmodelv1.TradingAccountPolicy{}).Order("id").Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("startup/rejected import changed row count to %d, want %d", len(after), len(before))
	}
	for i := range before {
		if !proto.Equal(before[i], after[i]) {
			t.Errorf("startup/rejected import changed policy %d", before[i].Id)
		}
	}
	extra := proto.CloneOf(batch.Profiles[0])
	extra.UserId, extra.FundId, extra.Symbol = 20001, 20001, "EXTRAUSDT"
	if err := store.ImportProfiles(ctx, &commonv1.VenueProfileImport{Profiles: []*commonv1.VenueProfilePolicy{extra}}); err != nil {
		t.Fatal(err)
	}
	cfg.Venue.SyntheticTape.Profiles = append(cfg.Venue.SyntheticTape.Profiles, &configv1.SyntheticTapeProfile{
		Symbol:          extra.Symbol,
		Quantity:        "0.01",
		MaximumNotional: "100",
	})
	loaded, err := venueconfig.LoadRuntime(ctx, db, cfg, nil)
	if err != nil || len(loaded.Venue.Profiles) != 258 {
		t.Fatalf("LoadRuntime(258 stored markets) = %v, want every profile without truncation", err)
	}
}
