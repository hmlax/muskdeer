//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	natsjs "github.com/nats-io/nats.go/jetstream"

	"github.com/hmlax/muskdeer/deer/corelocal"
	commonmodelv1 "github.com/hmlax/muskdeer/deer/proto/common/model/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/proto/execution/model/v1"
	strategymodelv1 "github.com/hmlax/muskdeer/deer/proto/strategy/model/v1"
)

func TestCoreLocalSeedIsIdempotentAndNeverFundsOrEnablesTrading(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	first, err := corelocal.Seed(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&commonmodelv1.AccountBudget{}).Where("account_id = ?", corelocal.AccountID).
		Updates(map[string]any{"limit_notional": "7.123456789012345678", "revision": 2}).Error; err != nil {
		t.Fatal(err)
	}
	second, err := corelocal.Seed(ctx, db)
	if err != nil || first.BinanceFeedID != second.BinanceFeedID || first.OKXFeedID != second.OKXFeedID {
		t.Fatalf("repeat changed local resource identities: %v", err)
	}
	var budget commonmodelv1.AccountBudget
	if err := db.WithContext(ctx).Model(&commonmodelv1.AccountBudget{}).Where("account_id = ?", corelocal.AccountID).First(&budget).Error; err != nil {
		t.Fatal(err)
	}
	if budget.Enabled || budget.Revision != 2 || budget.LimitNotional != "7.123456789012345678000000000000000000" {
		t.Fatal("repeat reset an existing budget or enabled risk")
	}
	for _, model := range []any{&executionmodelv1.SimulatorBalance{}, &strategymodelv1.StrategyInstance{}, &executionmodelv1.OrderIntent{}, &executionmodelv1.SimulatorOrder{}} {
		var count int64
		if err := db.WithContext(ctx).Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("local startup created money, a strategy or orders: count=%d err=%v", count, err)
		}
	}
	var account executionmodelv1.SimulatorAccount
	if err := db.WithContext(ctx).Model(&executionmodelv1.SimulatorAccount{}).Where("account_id = ?", corelocal.AccountID).First(&account).Error; err != nil || account.Enabled {
		t.Fatal("startup enabled a simulated trading account", err)
	}
}

func TestCoreLocalBrokerKeepsExistingConsumerContractOnConflict(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	cfg := businessStream(ctx, t)
	cfg.Stream += "_local"
	cfg.SubjectPrefix += "_local"
	if err := corelocal.PrepareBroker(ctx, cfg, "portfolio_local"); err != nil {
		t.Fatal(err)
	}
	if err := corelocal.PrepareBroker(ctx, cfg, "portfolio_local"); err != nil {
		t.Fatal("repeat changed broker resource contract", err)
	}
	conn, err := nats.Connect("nats://"+cfg.Address, testNATSAuth(), nats.NoReconnect(), nats.Timeout(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	js, err := natsjs.New(conn)
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := js.Consumer(ctx, cfg.Stream, "portfolio_local")
	if err != nil {
		t.Fatal(err)
	}
	config := consumer.CachedInfo().Config
	config.MaxAckPending = 1
	if _, err := js.UpdateConsumer(ctx, cfg.Stream, config); err != nil {
		t.Fatal(err)
	}
	if err := corelocal.PrepareBroker(ctx, cfg, "portfolio_local"); !errors.Is(err, corelocal.ErrResources) {
		t.Fatal("incompatible existing consumer was adopted", err)
	}
	current, err := consumer.Info(ctx)
	if err != nil || current.Config.MaxAckPending != 1 {
		t.Fatal("conflict changed existing consumer configuration", err)
	}
}
