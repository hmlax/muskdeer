//go:build integration

package migrate

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	commonmodelv1 "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	strategymodelv1 "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

func TestVenueStrategyVersionsAreTheOnlyRuntimeParameterSource(t *testing.T) {
	db, store, lease, _ := sdkAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := venueconfig.LoadBusiness(ctx, db); err == nil {
		t.Fatal("LoadBusiness(empty strategy catalog) accepted missing parameters")
	}
	if err := db.WithContext(ctx).Model(&commonmodelv1.ExecutionShardLease{}).Where("account_id = ?", lease.Key.AccountID).UpdateColumn("expires_at", 0).Error; err != nil {
		t.Fatal(err)
	}
	profile := databaseProfileFixture()
	if err := store.ImportProfiles(ctx, &commonv1.VenueProfileImport{Profiles: []*commonv1.VenueProfilePolicy{profile}}); err != nil {
		t.Fatal(err)
	}
	parameters := &configv1.BusinessRuntime{
		Enabled:          true,
		ExecutionVenue:   native.Venue,
		PollInterval:     durationpb.New(200 * time.Millisecond),
		OperationTimeout: durationpb.New(2 * time.Second),
		Venue: &configv1.VenueRuntime{
			Depth:               20,
			QuantityFraction:    "0.1",
			ReplacementFraction: "0.1",
			BridgeSlots:         1,
			LeaseTtl:            durationpb.New(10 * time.Second),
			Symbols:             []string{profile.Symbol},
		},
	}
	input := &commonv1.VenueStrategyImport{Strategies: []*commonv1.VenueStrategyInput{{
		Profile:         profile,
		Parameters:      parameters,
		BaseAsset:       "BTC",
		QuoteAsset:      "USDT",
		MinimumNotional: "5",
	}}}
	if err := venueconfig.ImportStrategies(ctx, db, input); err != nil {
		t.Fatalf("ImportStrategies(reviewed BTC policy) = %v, want success", err)
	}
	loaded, err := venueconfig.LoadBusiness(ctx, db)
	if err != nil || !proto.Equal(loaded, parameters) {
		t.Fatalf("LoadBusiness(BTC) = %v, %v, want stored parameters", loaded, err)
	}
	if err := venueconfig.ImportStrategies(ctx, db, input); err != nil {
		t.Fatalf("ImportStrategies(exact replay) = %v, want no new version", err)
	}
	var versions []*strategymodelv1.StrategyConfigVersion
	if err := db.WithContext(ctx).Model(&strategymodelv1.StrategyConfigVersion{}).Where("parameters_schema_version = ?", 2).Find(&versions).Error; err != nil || len(versions) != 1 {
		t.Fatalf("strategy versions after replay = %d, %v, want 1", len(versions), err)
	}
	parameters.Venue.QuantityFraction = "0.2"
	input.Strategies[0].ExpectedRevision = 1
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := leases.Acquire(ctx, domain.LeaseKey{
		Kind:      domain.LeaseKindExecution,
		AccountID: native.MarketKey(profile.Symbol),
	}, lease.OwnerID, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := venueconfig.ImportStrategies(ctx, db, input); !errors.Is(err, domain.ErrLeaseHeld) {
		t.Fatalf("ImportStrategies(active market owner) = %v, want ErrLeaseHeld", err)
	}
	if err := db.WithContext(ctx).Model(&commonmodelv1.ExecutionShardLease{}).Where("account_id = ?", native.MarketKey(profile.Symbol)).UpdateColumn("expires_at", 0).Error; err != nil {
		t.Fatal(err)
	}
	if err := venueconfig.ImportStrategies(ctx, db, input); err != nil {
		t.Fatal(err)
	}
	if loaded.Venue.QuantityFraction != "0.1" {
		t.Fatal("maintenance hot-switched an existing startup snapshot")
	}
	restarted, err := venueconfig.LoadBusiness(ctx, db)
	if err != nil || restarted.GetVenue().GetQuantityFraction() != "0.2" {
		t.Fatalf("LoadBusiness(explicit restart) = %v, %v, want revised parameters", restarted, err)
	}
	err = db.WithContext(ctx).Model(&strategymodelv1.StrategyConfigVersion{}).Where("id = ?", versions[0].Id).UpdateColumn("maker_parameters", &commonv1.MakerParameters{VenueRuntime: parameters}).Error
	var constraint *pgconn.PgError
	if !errors.As(err, &constraint) || constraint.Code != "23514" || constraint.ConstraintName != "venue_strategy_version_immutable" {
		t.Fatalf("rewrite immutable venue history = %v, want venue_strategy_version_immutable constraint", err)
	}
	err = db.WithContext(ctx).Model(&strategymodelv1.StrategyConfigVersion{}).Where("id = ?", versions[0].Id).Delete(&strategymodelv1.StrategyConfigVersion{}).Error
	constraint = nil
	if !errors.As(err, &constraint) || constraint.Code != "23514" || constraint.ConstraintName != "venue_strategy_version_immutable" {
		t.Fatalf("delete immutable venue history = %v, want venue_strategy_version_immutable constraint", err)
	}
}

func TestVenueStrategyBatchLoadsEverySelectedMarketAndRollsBackConflicts(t *testing.T) {
	db, store, lease, _ := sdkAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if err := db.WithContext(ctx).Model(&commonmodelv1.ExecutionShardLease{}).Where("account_id = ?", lease.Key.AccountID).UpdateColumn("expires_at", 0).Error; err != nil {
		t.Fatal(err)
	}
	batch := &commonv1.VenueStrategyImport{}
	for i := range 19 {
		profile := databaseProfileFixture()
		profile.UserId += int64(i)
		profile.FundId += int64(i)
		if i > 0 {
			profile.Symbol = fmt.Sprintf("COIN%dUSDT", i)
		}
		batch.Strategies = append(batch.Strategies, &commonv1.VenueStrategyInput{
			Profile:         profile,
			BaseAsset:       "COIN",
			QuoteAsset:      "USDT",
			MinimumNotional: "5",
			Parameters: &configv1.BusinessRuntime{
				Enabled:          true,
				ExecutionVenue:   native.Venue,
				PollInterval:     durationpb.New(200 * time.Millisecond),
				OperationTimeout: durationpb.New(2 * time.Second),
				Venue: &configv1.VenueRuntime{
					Depth:               20,
					QuantityFraction:    "0.1",
					ReplacementFraction: "0.1",
					BridgeSlots:         1,
					LeaseTtl:            durationpb.New(10 * time.Second),
					Symbols:             []string{profile.Symbol},
					SyntheticTape: &configv1.SyntheticTape{
						Enabled:         true,
						DevelopmentOnly: true,
						RunId:           "batch-test",
						Interval:        durationpb.New(time.Second),
						MaximumEvents:   2,
						Profiles: []*configv1.SyntheticTapeProfile{{
							Symbol:          profile.Symbol,
							Quantity:        "0.01",
							MaximumNotional: "100",
						}},
					},
				},
			},
		})
	}
	if err := store.InitializeStrategies(ctx, batch); err != nil {
		t.Fatalf("ImportStrategies(19 markets) = %v, want success", err)
	}
	loaded, err := venueconfig.LoadBusiness(ctx, db)
	if err != nil || len(loaded.GetVenue().GetSymbols()) != 19 || len(loaded.GetVenue().GetSyntheticTape().GetProfiles()) != 19 {
		t.Fatalf("LoadBusiness(19 markets) = %v, %v, want all nineteen", loaded, err)
	}
	settings, err := venueconfig.LoadRuntime(ctx, db, loaded, nil)
	if err != nil || len(settings.Venue.Profiles) != 19 {
		t.Fatalf("LoadRuntime(19 selected markets) = %v, %v, want all nineteen", settings, err)
	}
	var before []*commonmodelv1.TradingAccountPolicy
	if err := db.WithContext(ctx).Model(&commonmodelv1.TradingAccountPolicy{}).Order("id").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	changed := proto.CloneOf(batch)
	changed.Strategies[0].Profile.MaximumNotional = "999"
	for _, input := range changed.Strategies {
		input.ExpectedRevision = 1
		input.Parameters.Venue.QuantityFraction = "0.2"
	}
	changed.Strategies[len(changed.Strategies)-1].ExpectedRevision = 7
	if err := store.InitializeStrategies(ctx, changed); err == nil {
		t.Fatal("ImportStrategies(stale final revision) committed the batch")
	}
	again, err := venueconfig.LoadBusiness(ctx, db)
	if err != nil || !proto.Equal(loaded, again) {
		t.Fatalf("failed batch changed earlier strategy versions: %v", err)
	}
	var after []*commonmodelv1.TradingAccountPolicy
	if err := db.WithContext(ctx).Model(&commonmodelv1.TradingAccountPolicy{}).Order("id").Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Fatalf("failed strategy batch changed financial row count: %d, want %d", len(after), len(before))
	}
	for i := range before {
		if !proto.Equal(before[i], after[i]) {
			t.Fatalf("failed strategy batch changed financial policy %d", before[i].Id)
		}
	}
	var count int64
	if err := db.WithContext(ctx).Model(&strategymodelv1.StrategyConfigVersion{}).Where("parameters_schema_version = ?", 2).Count(&count).Error; err != nil || count != 19 {
		t.Fatalf("versions after stale batch = %d, %v, want 19", count, err)
	}
}
