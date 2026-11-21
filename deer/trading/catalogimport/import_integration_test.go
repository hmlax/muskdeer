//go:build integration

package catalogimport

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/migrate/schemaformat"
	config "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/postgres"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/strategyregistry"
)

func TestPreviewApplyReplayAndConflictPreserveExistingState(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	db := isolatedDatabase(ctx, t)
	entry := example("币安人生USDT", 71001)
	input := &Input{Entries: []*Entry{entry}}
	result, err := Import(ctx, db, input, false)
	if err != nil || result.Applied || result.Instances != 2 || result.Accounts != 1 || result.Instruments != 1 {
		t.Fatalf("preview: %+v, %v", result, err)
	}
	assertCounts(ctx, t, db, 0, 0, 0)
	result, err = Import(ctx, db, input, true)
	if err != nil || !result.Applied || result.Instances != 2 {
		t.Fatalf("apply: %+v, %v", result, err)
	}
	assertCounts(ctx, t, db, 1, 2, 2)
	var policy model.TradingAccountPolicy
	if err := db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Take(&policy).Error; err != nil || policy.Enabled {
		t.Fatalf("new account must be disabled: %v", err)
	}
	var running int64
	if err := db.WithContext(ctx).Model(&strategy.StrategyInstance{}).Where("desired_running = ?", true).Count(&running).Error; err != nil || running != 0 {
		t.Fatalf("new instances must be stopped: %v", err)
	}
	// A later user activation/financial observation must survive an import replay.
	if err := db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Where("id = ?", policy.Id).Updates(map[string]any{"enabled": true, "available_quote": "777", "reserved_notional": "3"}).Error; err != nil {
		t.Fatal(err)
	}
	var maker strategy.StrategyInstance
	if err := db.WithContext(ctx).Model(&strategy.StrategyInstance{}).Where("instance_key = ?", entry.Maker.InstanceKey).Take(&maker).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := instanceconfig.ManageConfigured(ctx, db, instanceconfig.ManagementWrite{
		Operation:              instanceconfig.ManagementStart,
		Subject:                importSubject,
		RequestID:              "test-activate-before-replay",
		ID:                     maker.Id,
		ExpectedRevision:       maker.Revision,
		ExpectedControlVersion: maker.RequestedConfigVersion,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Take(&policy).Error; err != nil {
		t.Fatal(err)
	}
	before := proto.CloneOf(&policy)
	result, err = Import(ctx, db, input, true)
	if err != nil || result.Instances != 0 || result.Accounts != 0 || result.Instruments != 0 {
		t.Fatalf("idempotent replay: %+v, %v", result, err)
	}
	if err := db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Take(&policy).Error; err != nil || !proto.Equal(before, &policy) {
		t.Fatalf("account state changed on replay: %v", err)
	}
	if err := db.WithContext(ctx).Model(&strategy.StrategyInstance{}).Where("id = ?", maker.Id).Take(&maker).Error; err != nil || !maker.DesiredRunning || maker.RequestedConfigVersion != 2 {
		t.Fatalf("instance controls changed on replay: %v", err)
	}
	assertCounts(ctx, t, db, 1, 2, 3)
	// A bad tail rolls back the preceding new product, account, instances and audit.
	invalid := example("BADUSDT", 71003)
	invalid.Wash.TemplateRevision = 999
	_, err = Import(ctx, db, &Input{Entries: []*Entry{example("NEWUSDT", 71002), invalid}}, true)
	if err == nil {
		t.Fatal("invalid tail was accepted")
	}
	assertCounts(ctx, t, db, 1, 2, 3)
	conflict := example("币安人生USDT", 71001)
	conflict.Profile.MaximumNotional = "2000"
	if _, err := Import(ctx, db, &Input{Entries: []*Entry{conflict}}, true); err == nil {
		t.Fatal("existing policy was silently overwritten")
	}
	invalid = example("LIVEUSDT", 71004)
	invalid.Maker.DesiredRunning = true
	if _, err := Import(ctx, db, &Input{Entries: []*Entry{invalid}}, true); err == nil {
		t.Fatal("running import was accepted")
	}
	assertCounts(ctx, t, db, 1, 2, 3)
}

func assertCounts(ctx context.Context, t *testing.T, db *gorm.DB, accounts, instances, history int64) {
	t.Helper()
	var products, policies, rows, versions int64
	if err := db.WithContext(ctx).Model(&model.Instrument{}).Count(&products).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Count(&policies).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&strategy.StrategyInstance{}).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&strategy.StrategyConfigVersion{}).Count(&versions).Error; err != nil {
		t.Fatal(err)
	}
	if products != accounts || policies != accounts || rows != instances || versions != history {
		t.Fatalf("counts products=%d policies=%d instances=%d versions=%d; want %d/%d/%d/%d", products, policies, rows, versions, accounts, accounts, instances, history)
	}
}

func example(symbol string, user int64) *Entry {
	parameters := func() *common.StrategyInstanceParameters {
		return &common.StrategyInstanceParameters{
			SchemaVersion: 1,
			Execution: &common.StrategyExecutionParameters{
				PollIntervalMs:     200,
				OperationTimeoutMs: 2000,
				LeaseTtlMs:         10000,
				Leverage:           10,
			},
		}
	}
	maker, wash := parameters(), parameters()
	maker.MarketMaker = &common.DepthMakerParameters{
		Depth:               20,
		QuantityFraction:    "0.1",
		ReplacementFraction: "0.1",
		BridgeSlots:         1,
	}
	wash.WashTrade = &common.WashTradeParameters{
		DevelopmentOnly:  true,
		Mode:             common.WashTradeMode_WASH_TRADE_MODE_AGG_TRADE,
		RunId:            "catalog-import-test",
		Quantity:         "1",
		MaximumNotional:  "0",
		QuantityFraction: "0.1",
	}
	return &Entry{
		Instrument: &model.Instrument{
			ExecutionVenue:  native.Venue,
			Symbol:          symbol,
			BaseAsset:       strings.TrimSuffix(symbol, "USDT"),
			QuoteAsset:      "USDT",
			PriceTick:       "0.01",
			QuantityStep:    "0.001",
			MinNotional:     "1",
			Status:          domain.InstrumentStatusTrading,
			MetadataVersion: 1,
		},
		Profile: &common.VenueProfilePolicy{
			UserId:               user,
			FundId:               user,
			Symbol:               symbol,
			MaximumNotional:      "1000",
			MaximumOrderNotional: "100",
			Leverage:             10,
			MarketDataSource:     native.Source,
			PriceTick:            "0.01",
			QuantityStep:         "0.001",
		},
		Maker: &common.StrategyInstanceInput{
			InstanceKey:      "venue/" + symbol,
			TemplateId:       strategyregistry.MakerTemplateID,
			TemplateRevision: 1,
			MarketDataSource: native.Source,
			Parameters:       maker,
		},
		Wash: &common.StrategyInstanceInput{
			InstanceKey:      "wash/" + symbol,
			TemplateId:       strategyregistry.WashTradeTemplateID,
			TemplateRevision: 1,
			MarketDataSource: native.Source,
			Parameters:       wash,
		},
	}
}

func isolatedDatabase(ctx context.Context, t *testing.T) *gorm.DB {
	t.Helper()
	database := os.Getenv("MUSKDEER_TEST_POSTGRES_DATABASE")
	port, err := strconv.ParseUint(os.Getenv("MUSKDEER_TEST_POSTGRES_PORT"), 10, 16)
	if err != nil || !strings.HasPrefix(database, "muskdeer_test_") {
		t.Fatal("isolated PostgreSQL test database required")
	}
	client, err := postgres.New(ctx, &config.Postgres{
		Host:             "127.0.0.1",
		Port:             uint32(port),
		Database:         database,
		Username:         os.Getenv("MUSKDEER_TEST_POSTGRES_USERNAME"),
		PasswordEnv:      "MUSKDEER_TEST_POSTGRES_PASSWORD",
		SslMode:          postgres.SSLModeDisable,
		Schema:           "public",
		MaxOpenConns:     2,
		MaxIdleConns:     1,
		ConnectTimeout:   durationpb.New(time.Second),
		StatementTimeout: durationpb.New(5 * time.Second),
		MaxLifetime:      durationpb.New(time.Minute),
		MaxIdleTime:      durationpb.New(time.Minute),
	}, "catalog-import-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := schemaformat.Install(ctx, client.GORM()); err != nil {
		t.Fatal(err)
	}
	return client.GORM()
}
