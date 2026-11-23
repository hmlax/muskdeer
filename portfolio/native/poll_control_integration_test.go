//go:build integration

package native

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/migrate/schemaformat"
	"github.com/hmlax/muskdeer/deer/migrate/testschema"
	config "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/postgres"
	values "github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

func TestDisabledCatalogFundsDoNotPollSDKAndEnableWithoutReload(t *testing.T) {
	db := pollingDatabase(t)
	ctx := t.Context()
	parent := &Runtime{
		db:       db,
		children: make(map[string]*Runtime),
	}
	for _, fund := range []int64{1, 2, 3, 4} {
		profile := &common.VenueProfilePolicy{
			UserId: fund,
			FundId: fund,
			Symbol: "BTCUSDT",
		}
		key := values.ScopeKey(fund, fund)
		cfg := &venueconfig.RuntimeConfig{
			InstanceID: fund,
			Venue: &common.VenuePolicy{
				Profiles: []*common.VenueProfilePolicy{profile},
			},
		}
		if fund == 3 {
			cfg.LongGrid = &common.LongGridParameters{}
		}
		parent.children[key] = &Runtime{
			db:              db,
			cfg:             cfg,
			observeEnabled:  &atomic.Bool{},
			observeRecovery: fund == 4,
		}
		row := &model.TradingAccountPolicy{
			AccountId:            key,
			UserId:               fund,
			FundId:               fund,
			Enabled:              fund == 2,
			MaximumNotional:      "100",
			MaximumOrderNotional: "100",
			ObservedNotional:     "0",
			ReservedNotional:     "0",
			AvailableQuote:       "0",
			WorstFeeRate:         "0",
			PriceTick:            "1",
			QuantityStep:         "1",
			Leverage:             10,
			Symbol:               "BTCUSDT",
			MarketDataSource:     values.Source,
		}
		if err := db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := parent.refreshPolling(ctx); err != nil {
		t.Fatal(err)
	}
	paused := parent.children[values.ScopeKey(1, 1)]
	if paused.observeEnabled.Load() || !parent.children[values.ScopeKey(2, 2)].observeEnabled.Load() || !parent.children[values.ScopeKey(3, 3)].observeEnabled.Load() || !parent.children[values.ScopeKey(4, 4)].observeEnabled.Load() {
		t.Fatal("disabled/active/grid observation admission is incorrect")
	}
	// No factory is supplied: reaching any RPC dependency panics. A stopped,
	// disabled catalog fund must return before touching that dependency.
	if err := paused.poll(ctx, paused.cfg.Venue.Profiles[0]); err != nil {
		t.Fatalf("poll(disabled fund) = %v, want no remote work", err)
	}
	for _, enabled := range []bool{true, false} {
		if err := db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Where("account_id = ?", values.ScopeKey(1, 1)).Update("enabled", enabled).Error; err != nil {
			t.Fatal(err)
		}
		if err := parent.refreshPolling(ctx); err != nil || paused.observeEnabled.Load() != enabled {
			t.Fatalf("refreshPolling(enabled=%t) = %v/%t", enabled, err, paused.observeEnabled.Load())
		}
	}
	var joined sync.WaitGroup
	joined.Go(func() {
		for range 50 {
			if err := paused.poll(ctx, paused.cfg.Venue.Profiles[0]); err != nil {
				t.Error(err)
			}
		}
	})
	for range 10 {
		if err := parent.refreshPolling(ctx); err != nil {
			t.Error(err)
		}
	}
	joined.Wait()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := parent.refreshPolling(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("refreshPolling(cancelled) = %v, want cancellation", err)
	}
	if !parent.children[values.ScopeKey(2, 2)].observeEnabled.Load() {
		t.Fatal("failed control read erased last known observation admission")
	}
	if err := parent.watchPolling(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("watchPolling(cancelled) = %v, want cancellation", err)
	}
	if len(parent.Workers()) != 5 || len(parent.children) != 4 {
		t.Fatal("disabled fund was removed instead of remaining dynamically loadable")
	}
}

func pollingDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	database := os.Getenv("MUSKDEER_TEST_POSTGRES_DATABASE")
	port, err := strconv.ParseUint(os.Getenv("MUSKDEER_TEST_POSTGRES_PORT"), 10, 16)
	if err != nil || !strings.HasPrefix(database, "muskdeer_test_") {
		t.Fatal("isolated PostgreSQL test database required")
	}
	schema := "muskdeer_test_" + strings.ToLower(rand.Text())
	connection, err := postgres.New(ctx, &config.Postgres{
		Host:             "127.0.0.1",
		Port:             uint32(port),
		Database:         database,
		Username:         os.Getenv("MUSKDEER_TEST_POSTGRES_USERNAME"),
		PasswordEnv:      "MUSKDEER_TEST_POSTGRES_PASSWORD",
		SslMode:          postgres.SSLModeDisable,
		Schema:           schema,
		MaxOpenConns:     4,
		MaxIdleConns:     2,
		ConnectTimeout:   durationpb.New(time.Second),
		StatementTimeout: durationpb.New(5 * time.Second),
		MaxLifetime:      durationpb.New(time.Minute),
		MaxIdleTime:      durationpb.New(time.Minute),
	}, "portfolio-poll-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := connection.Close(); err != nil {
			t.Error(err)
		}
	})
	db := connection.GORM()
	if err := testschema.Create(ctx, db, schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
		defer cancel()
		if err := testschema.Drop(ctx, db, schema); err != nil {
			t.Error(err)
		}
	})
	if err := schemaformat.Install(ctx, db); err != nil {
		t.Fatal(err)
	}
	return db
}
