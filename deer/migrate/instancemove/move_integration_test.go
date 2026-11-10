//go:build integration

package instancemove

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hmlax/muskdeer/deer/migrate"
	"github.com/hmlax/muskdeer/deer/migrate/schemaformat"
	venueMigration "github.com/hmlax/muskdeer/deer/migrate/venue"
	"github.com/hmlax/muskdeer/deer/ownerid"
	config "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	values "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/postgres"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
	"github.com/hmlax/muskdeer/deer/trading/strategyregistry"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
	"github.com/hmlax/muskdeer/execution/adapters/himalaya"
	venue "github.com/hmlax/muskdeer/execution/venue"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"gorm.io/gorm"
)

func database(t *testing.T) *gorm.DB {
	t.Helper()
	port, err := strconv.ParseUint(os.Getenv("MUSKDEER_TEST_POSTGRES_PORT"), 10, 16)
	if err != nil || port == 0 || os.Getenv("MUSKDEER_TEST_POSTGRES_PASSWORD") == "" {
		t.Fatal("isolated PostgreSQL environment required")
	}
	name := "instance_" + strings.ToLower(rand.Text())
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	client, err := postgres.New(ctx, &config.Postgres{
		Host:             "127.0.0.1",
		Port:             uint32(port),
		Database:         os.Getenv("MUSKDEER_TEST_POSTGRES_DATABASE"),
		Username:         os.Getenv("MUSKDEER_TEST_POSTGRES_USERNAME"),
		PasswordEnv:      "MUSKDEER_TEST_POSTGRES_PASSWORD",
		SslMode:          postgres.SSLModeDisable,
		Schema:           name,
		MaxOpenConns:     4,
		MaxIdleConns:     2,
		ConnectTimeout:   durationpb.New(time.Second),
		StatementTimeout: durationpb.New(5 * time.Second),
		MaxLifetime:      durationpb.New(time.Minute),
		MaxIdleTime:      durationpb.New(time.Minute),
	}, "instance-migration-test")
	if err != nil {
		t.Fatal(err)
	}
	db := client.GORM()
	identifier := pgx.Identifier{name}.Sanitize()
	if err := db.WithContext(ctx).Model(&common.SchemaMigration{}).Exec("CREATE SCHEMA " + identifier).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
		defer stop()
		if err := db.WithContext(cleanup).Model(&common.SchemaMigration{}).Exec("DROP SCHEMA " + identifier + " CASCADE").Error; err != nil {
			t.Error(err)
		}
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}
func legacy(ctx context.Context, t *testing.T) *gorm.DB {
	t.Helper()
	db := database(t)
	if _, err := migrate.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := venueMigration.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	account := native.ScopeKey(10001, 10001)
	if err := db.WithContext(ctx).Model(&common.ExecutionShardLease{}).Create(&common.ExecutionShardLease{
		AccountId: account,
		OwnerId:   "legacy/source",
		Epoch:     1,
		ExpiresAt: 0,
	}).Error; err != nil {
		t.Fatal(err)
	}
	budget := &common.TradingAccountPolicy{
		AccountId:            account,
		UserId:               10001,
		FundId:               10001,
		MaximumNotional:      "1000",
		ObservedNotional:     "0",
		ReservedNotional:     "0",
		SnapshotExpiresAt:    time.Now().Add(time.Minute).UnixMilli(),
		Enabled:              true,
		AvailableQuote:       "1000",
		MaximumOrderNotional: "100",
		Leverage:             10,
		WorstFeeRate:         "0.001",
		CutoverCompleted:     true,
		Symbol:               "BTCUSDT",
		MarketDataSource:     native.Source,
		PriceTick:            "0.1",
		QuantityStep:         "0.001",
	}
	if err := db.WithContext(ctx).Model(&common.TradingAccountPolicy{}).Create(budget).Error; err != nil {
		t.Fatal(err)
	}
	input := &values.VenueStrategyImport{Strategies: []*values.VenueStrategyInput{{
		Profile: &values.VenueProfilePolicy{
			UserId:               10001,
			FundId:               10001,
			Symbol:               "BTCUSDT",
			MarketDataSource:     native.Source,
			PriceTick:            "0.1",
			QuantityStep:         "0.001",
			Leverage:             10,
			MaximumNotional:      "1000",
			MaximumOrderNotional: "100",
		},
		BaseAsset:       "BTC",
		QuoteAsset:      "USDT",
		MinimumNotional: "1",
		Parameters: &config.BusinessRuntime{
			Enabled:          true,
			ExecutionVenue:   native.Venue,
			PollInterval:     durationpb.New(200 * time.Millisecond),
			OperationTimeout: durationpb.New(time.Second),
			Venue: &config.VenueRuntime{
				Depth:               20,
				QuantityFraction:    "0.1",
				ReplacementFraction: "0.1",
				BridgeSlots:         1,
				LeaseTtl:            durationpb.New(10 * time.Second),
				Symbols:             []string{"BTCUSDT"},
				SyntheticTape: &config.SyntheticTape{
					Enabled:         true,
					DevelopmentOnly: true,
					Mode:            config.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE,
					RunId:           "migration-test",
					Interval:        durationpb.New(time.Second),
					MaximumEvents:   10,
					Profiles: []*config.SyntheticTapeProfile{{
						Symbol:          "BTCUSDT",
						Quantity:        "0.01",
						MaximumNotional: "100",
					}},
				},
			},
		},
	}}}
	if err := venueconfig.ImportStrategies(ctx, db, input); err != nil {
		t.Fatal(err)
	}
	return db
}
func TestMovePreservesUnknownFundsAuditAndConsumedTapeWithoutReplay(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	source := legacy(ctx, t)
	account := native.ScopeKey(10001, 10001)
	unknown := &execution.VenueOrderIntent{
		Id:               8555,
		AccountId:        account,
		UserId:           10001,
		FundId:           10001,
		Symbol:           "BTCUSDT",
		ClientId:         "unknown-preserved",
		OwnerId:          "legacy/source",
		OwnerEpoch:       1,
		RequestDigest:    strings.Repeat("a", 64),
		Side:             string(domain.SideBuy),
		Price:            "100",
		Quantity:         "1",
		Leverage:         10,
		State:            venue.IntentDispatched,
		ReservedNotional: "100",
	}
	if err := source.WithContext(ctx).Model(&execution.VenueOrderIntent{}).Create(unknown).Error; err != nil {
		t.Fatal(err)
	}
	if err := source.WithContext(ctx).Model(&common.TradingAccountPolicy{}).Where("account_id = ?", account).UpdateColumn("reserved_notional", "100").Error; err != nil {
		t.Fatal(err)
	}
	for sequence := range 2 {
		if err := source.WithContext(ctx).Model(&execution.SyntheticTapeIntent{}).Create(&execution.SyntheticTapeIntent{
			RunId:        "migration-test",
			Symbol:       "BTCUSDT",
			Sequence:     int64(sequence),
			PolicyDigest: strings.Repeat("b", 64),
			Plan: &values.SyntheticTapePlan{
				RunId:    "migration-test",
				Symbol:   "BTCUSDT",
				Sequence: uint64(sequence),
			},
			Notional: "1",
			Result:   &values.SyntheticTapeResult{State: native.SyntheticUnknown},
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	target := database(t)
	if err := Move(ctx, source, target); err != nil {
		t.Fatal(err)
	}
	if err := migrate.CheckCurrent(ctx, target); err != nil {
		t.Fatal(err)
	}
	if err := venueMigration.Check(ctx, target); err != nil {
		t.Fatal(err)
	}
	var before, after execution.VenueOrderIntent
	if err := source.WithContext(ctx).Model(&execution.VenueOrderIntent{}).Where("id = ?", unknown.Id).Take(&before).Error; err != nil {
		t.Fatal(err)
	}
	if err := target.WithContext(ctx).Model(&execution.VenueOrderIntent{}).Where("id = ?", unknown.Id).Take(&after).Error; err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(&before, &after) || after.HistoricalApprovalReference != "" {
		t.Fatal("unknown identity, state, approval or full reservation changed")
	}
	var originalBudget, copiedBudget common.TradingAccountPolicy
	if err := source.WithContext(ctx).Model(&common.TradingAccountPolicy{}).Take(&originalBudget).Error; err != nil {
		t.Fatal(err)
	}
	if err := target.WithContext(ctx).Model(&common.TradingAccountPolicy{}).Take(&copiedBudget).Error; err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(&originalBudget, &copiedBudget) {
		t.Fatal("financial policy changed")
	}
	instances, err := instanceconfig.Load(ctx, target)
	if err != nil || len(instances) != 2 {
		t.Fatalf("independent maker/wash startup: %d %v", len(instances), err)
	}
	for _, instance := range instances {
		if instance.Type == domain.StrategyTemplateWashTrade && (instance.ResumeSequence != 2 || instance.TapePolicyDigest != strings.Repeat("b", 64)) {
			t.Fatal("finite run was reset or policy identity replaced")
		}
	}
	if target.WithContext(ctx).Migrator().HasTable(&execution.SimulatorAccount{}) || target.WithContext(ctx).Migrator().HasTable(&common.ServiceRuntimePolicy{}) {
		t.Fatal("unused legacy tables recreated")
	}
	var history []*common.MigrationHistory
	if err := target.WithContext(ctx).Model(&common.MigrationHistory{}).Find(&history).Error; err != nil || len(history) < 40 {
		t.Fatalf("missing original migration audit: %v", err)
	}
	// Startup snapshots remain private after explicit offline changes.
	row := &strategy.StrategyInstance{}
	if err := target.WithContext(ctx).Model(row).Where("id = ?", instances[0].ID).UpdateColumn("implementation", "missing-v1").Error; err == nil {
		t.Fatal("database accepted an unregistered implementation")
	}
	if instances[0].Implementation != instanceconfig.ImplementationMaker {
		t.Fatal("a failed write mutated the startup snapshot")
	}
}
func TestMoveRejectsActiveSourceAndNonemptyUnusedDataAtomically(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	source := legacy(ctx, t)
	target := database(t)
	if err := source.WithContext(ctx).Model(&common.ExecutionShardLease{}).Where("account_id = ?", native.ScopeKey(10001, 10001)).UpdateColumn("expires_at", time.Now().Add(time.Minute).UnixMilli()).Error; err != nil {
		t.Fatal(err)
	}
	if err := Move(ctx, source, target); !errors.Is(err, ErrSource) {
		t.Fatalf("active source: %v", err)
	}
	if target.WithContext(ctx).Migrator().HasTable(&common.SchemaMigration{}) {
		t.Fatal("failed migration changed target")
	}
	if err := source.WithContext(ctx).Model(&common.ExecutionShardLease{}).Where("account_id = ?", native.ScopeKey(10001, 10001)).UpdateColumn("expires_at", 0).Error; err != nil {
		t.Fatal(err)
	}
	if err := source.WithContext(ctx).Model(&common.AccountBudget{}).Create(&common.AccountBudget{
		AccountId:        "unused-real-data",
		Asset:            "USDT",
		LimitNotional:    "1",
		ReservedNotional: "0",
		Revision:         1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := Move(ctx, source, target); !errors.Is(err, ErrSource) {
		t.Fatalf("unsupported useful data: %v", err)
	}
	if target.WithContext(ctx).Migrator().HasTable(&common.SchemaMigration{}) {
		t.Fatal("rejected data loss changed target")
	}
}
func TestFinalSDKBoundaryRejectsStaleInstanceRevision(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	source := legacy(ctx, t)
	target := database(t)
	if err := Move(ctx, source, target); err != nil {
		t.Fatal(err)
	}
	instances, err := instanceconfig.Load(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	instance := instances[0]
	leases, err := repository.NewLeases(target)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := ownerid.New(ctx, "instance-final-guard")
	if err != nil {
		t.Fatal(err)
	}
	acquire := func(key string) domain.LeaseToken {
		token, err := leases.Acquire(ctx, domain.LeaseKey{
			Kind:      domain.LeaseKindExecution,
			AccountID: key,
		}, owner, 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	executionLease := acquire(native.ScopeKey(10001, 10001))
	strategyLease := acquire(native.StrategyKey(10001, 10001))
	sourceLease := acquire(native.MarketKey("BTCUSDT"))
	store, err := venue.NewStore(target, himalaya.Factory{})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := store.PrepareLive(ctx, executionLease, venue.SubmitRequest{
		UserID:   10001,
		FundID:   10001,
		Symbol:   "BTCUSDT",
		ClientID: "current-instance-fence",
		Side:     domain.SideBuy,
		Price:    decimal.NewFromInt(100),
		Quantity: decimal.NewFromInt(1),
		Leverage: 10,
	}, venue.LiveAuthority{
		Strategy: native.Authority(strategyLease),
		Book: &values.VenueBook{
			Symbol:       "BTCUSDT",
			Source:       native.Source,
			Segment:      native.Segment,
			Publisher:    native.Authority(sourceLease),
			ValidUntilMs: time.Now().Add(5 * time.Second).UnixMilli(),
		},
		InstanceID: instance.ID,
		Revision:   instance.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := target.WithContext(ctx).Model(&strategy.StrategyInstance{}).Where("id = ?", instance.ID).UpdateColumn("revision", instance.Revision+1).Error; err != nil {
		t.Fatal(err)
	}
	calls := 0
	if err := store.WithSubmission(ctx, prepared.Admission, func(context.Context) error { calls++; return nil }); !errors.Is(err, domain.ErrLeaseLost) || calls != 0 {
		t.Fatalf("stale instance reached SDK: %d %v", calls, err)
	}
	if err := schemaformat.Check(ctx, target); err != nil {
		t.Fatal(err)
	}
}

func TestIndependentInstancesKeepDifferentControlsAndAllThreeAlgorithms(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	source := legacy(ctx, t)
	target := database(t)
	if err := Move(ctx, source, target); err != nil {
		t.Fatal(err)
	}
	originals, err := instanceconfig.Load(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	input := &values.StrategyInstanceImport{}
	for index := range 257 {
		parameters := proto.CloneOf(originals[0].Parameters)
		parameters.Execution.PollIntervalMs = 100 + int64(index)
		parameters.MarketMaker.Depth = uint32(1 + index%20)
		input.Instances = append(input.Instances, &values.StrategyInstanceInput{
			InstanceKey:      "maker/independent/" + strconv.Itoa(index),
			TemplateId:       strategyregistry.MakerTemplateID,
			TemplateRevision: 1,
			StrategyType:     string(domain.StrategyTemplateMarketMaker),
			Implementation:   instanceconfig.ImplementationMaker,
			MarketDataSource: native.Source,
			AccountId:        native.ScopeKey(10001, 10001),
			InstrumentId:     1,
			DesiredRunning:   true,
			Parameters:       parameters,
		})
	}
	grid := proto.CloneOf(originals[0].Parameters)
	grid.MarketMaker = nil
	grid.Grid = &values.GridParameters{
		LowerPrice:       "90",
		UpperPrice:       "110",
		GridCount:        5,
		TotalBudget:      "100",
		PerOrderNotional: "10",
		InventoryLimit:   "10",
		MaxMarketAgeMs:   1000,
	}
	input.Instances = append(input.Instances, &values.StrategyInstanceInput{
		InstanceKey:      "grid/independent",
		TemplateId:       strategyregistry.GridTemplateID,
		TemplateRevision: 1,
		StrategyType:     string(domain.StrategyTemplateGrid),
		Implementation:   instanceconfig.ImplementationGrid,
		MarketDataSource: native.Source,
		AccountId:        native.ScopeKey(10001, 10001),
		InstrumentId:     1,
		DesiredRunning:   true,
		Parameters:       grid,
	})
	if err := instanceconfig.Import(ctx, target, input); err != nil {
		t.Fatal(err)
	}
	loaded, err := instanceconfig.Load(ctx, target)
	if err != nil || len(loaded) != 260 {
		t.Fatalf("all independent instances: %d %v", len(loaded), err)
	}
	byKey := make(map[string]*instanceconfig.Instance)
	for _, instance := range loaded {
		byKey[instance.Key] = instance
	}
	for _, write := range input.Instances {
		actual := byKey[write.InstanceKey]
		if actual == nil || actual.Type != domain.StrategyTemplate(write.StrategyType) || !proto.Equal(actual.Parameters, write.Parameters) {
			t.Fatalf("independent controls changed for %s", write.InstanceKey)
		}
	}
	// Exact replay appends no audit versions; stale mixed edits roll back together.
	var before int64
	if err := target.WithContext(ctx).Model(&strategy.StrategyConfigVersion{}).Count(&before).Error; err != nil {
		t.Fatal(err)
	}
	if err := instanceconfig.Import(ctx, target, input); err != nil {
		t.Fatal(err)
	}
	var after int64
	if err := target.WithContext(ctx).Model(&strategy.StrategyConfigVersion{}).Count(&after).Error; err != nil || before != after {
		t.Fatal("exact replay created history")
	}
	invalid := proto.CloneOf(input)
	invalid.Instances[0].ExpectedRevision = 1
	invalid.Instances[0].Parameters.MarketMaker.QuantityFraction = "0.2"
	invalid.Instances[len(invalid.Instances)-1].ExpectedRevision = 999
	invalid.Instances[len(invalid.Instances)-1].Parameters.Execution.PollIntervalMs = 999
	if err := instanceconfig.Import(ctx, target, invalid); err == nil {
		t.Fatal("stale batch accepted")
	}
	reloaded, err := instanceconfig.Load(ctx, target)
	if err != nil || !proto.Equal(reloaded[2].Parameters, loaded[2].Parameters) {
		t.Fatal("failed batch partially changed parameters")
	}
}

func TestDatabaseTemplatesAreRequiredAndExplicitlyVersionBound(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	source := legacy(ctx, t)
	target := database(t)
	if err := Move(ctx, source, target); err != nil {
		t.Fatal(err)
	}
	var templates []*strategy.StrategyTemplate
	if err := target.WithContext(ctx).Model(&strategy.StrategyTemplate{}).Order("id").Find(&templates).Error; err != nil {
		t.Fatal(err)
	}
	if len(templates) != 4 {
		t.Fatal("three database strategy templates were not created")
	}
	for _, template := range templates {
		if !strategyregistry.Matches(template) {
			t.Fatal("stored template contract differs from implemented algorithm")
		}
	}
	before, err := instanceconfig.Load(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if before[0].TemplateID != strategyregistry.MakerTemplateID || !before[0].Requirements().Account || before[1].TemplateID != strategyregistry.WashTradeTemplateID || before[1].Requirements().Account || !before[1].Requirements().Trades {
		t.Fatal("instances did not use their database template dependencies")
	}
	if err := target.WithContext(ctx).Model(&strategy.StrategyTemplate{}).Where("id = ?", strategyregistry.MakerTemplateID).UpdateColumn("enabled", false).Error; err == nil {
		t.Fatal("template changed without a fenced revision")
	}
	// An unused template can be disabled; it is never silently recreated by startup.
	if err := target.WithContext(ctx).Model(&strategy.StrategyTemplate{}).Where("id = ?", strategyregistry.GridTemplateID).Updates(map[string]any{
		"enabled":  false,
		"revision": 2,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := instanceconfig.Load(ctx, target); err != nil {
		t.Fatal(err)
	}
	if err := target.WithContext(ctx).Model(&strategy.StrategyTemplate{}).Where("id = ?", strategyregistry.GridTemplateID).Delete(&strategy.StrategyTemplate{}).Error; err != nil {
		t.Fatal(err)
	}
	parameters := proto.CloneOf(before[0].Parameters)
	parameters.MarketMaker = nil
	parameters.Grid = &values.GridParameters{
		LowerPrice:       "90",
		UpperPrice:       "110",
		GridCount:        5,
		TotalBudget:      "100",
		PerOrderNotional: "10",
		InventoryLimit:   "10",
		MaxMarketAgeMs:   1000,
	}
	if err := instanceconfig.Import(ctx, target, &values.StrategyInstanceImport{Instances: []*values.StrategyInstanceInput{{
		InstanceKey:      "missing-template",
		TemplateId:       strategyregistry.GridTemplateID,
		TemplateRevision: 2,
		StrategyType:     string(domain.StrategyTemplateGrid),
		Implementation:   instanceconfig.ImplementationGrid,
		MarketDataSource: native.Source,
		AccountId:        native.ScopeKey(10001, 10001),
		InstrumentId:     1,
		DesiredRunning:   true,
		Parameters:       parameters,
	}}}); err == nil {
		t.Fatal("instance ran without a database template")
	}
	var count int64
	if err := target.WithContext(ctx).Model(&strategy.StrategyInstance{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatal("missing-template import partially committed")
	}
	if err := instanceconfig.Import(ctx, target, &values.StrategyInstanceImport{Instances: []*values.StrategyInstanceInput{{
		InstanceKey:      "selected-template",
		TemplateId:       strategyregistry.MakerTemplateID,
		TemplateRevision: 1,
		MarketDataSource: native.Source,
		AccountId:        native.ScopeKey(10001, 10001),
		InstrumentId:     1,
		DesiredRunning:   true,
		Parameters:       before[0].Parameters,
	}}}); err != nil {
		t.Fatal(err)
	}
	var selected strategy.StrategyInstance
	if err := target.WithContext(ctx).Model(&strategy.StrategyInstance{}).Where("instance_key = ?", "selected-template").Take(&selected).Error; err != nil || selected.Template != string(domain.StrategyTemplateMarketMaker) || selected.Implementation != strategyregistry.ImplementationMaker {
		t.Fatal("instance did not select its template's registered implementation")
	}

}

func TestMovePreservesSequenceHighWater(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	source := legacy(ctx, t)
	statement := &gorm.Statement{DB: source}
	if err := statement.Parse(&common.ExecutionShardLease{}); err != nil {
		t.Fatal(err)
	}
	if err := source.WithContext(ctx).Model(&common.ExecutionShardLease{}).Exec("SELECT setval(pg_get_serial_sequence(?, 'id'), ?, true)", statement.Schema.Table, int64(123456)).Error; err != nil {
		t.Fatal(err)
	}
	target := database(t)
	if err := Move(ctx, source, target); err != nil {
		t.Fatal(err)
	}
	next := &common.ExecutionShardLease{
		AccountId: "next/identity",
		OwnerId:   "retained/highwater",
		Epoch:     1,
		ExpiresAt: 0,
	}
	if err := target.WithContext(ctx).Model(&common.ExecutionShardLease{}).Create(next).Error; err != nil || next.Id <= 123456 {
		t.Fatalf("sequence high water was reset: %d %v", next.Id, err)
	}

}
