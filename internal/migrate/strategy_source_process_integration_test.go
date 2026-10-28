//go:build integration

package migrate

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestStrategyProcessesProtectOnlyTheLostSourceAndRecoverOriginalSelection(t *testing.T) {
	for _, lost := range []domain.MarketSource{domain.MarketSourceBinance, domain.MarketSourceOKX} {
		t.Run(string(lost), func(t *testing.T) { strategySourceProcessScenario(t, lost) })
	}
}

func strategySourceProcessScenario(t *testing.T, lost domain.MarketSource) {
	t.Helper()
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	traceProcessLocks(ctx, t, db, []string{
		"strategy/business-strategy.source-protection",
		"execution/business-execution",
		"market/business-market-protect-binance",
		"market/business-market-protect-okx",
	})
	_, submit := admissionFixture(ctx, t, db)
	portfolioFixture(ctx, t, db, submit)
	binance, publisher, _, _ := admissionSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	expireLeaseFixture(ctx, t, db, publisher.Key)
	expireLeaseFixture(ctx, t, db, submit.ExecutionAuthority.Key)
	expireLeaseFixture(ctx, t, db, lifecycleToken(submit.Input.Authority).Key)
	feeds, err := repository.NewMarketFeeds(db)
	if err != nil {
		t.Fatal(err)
	}
	input := binance
	input.ID, input.Source, input.Symbol = 0, domain.MarketSourceOKX, "BTC-USDT"
	okx, err := feeds.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	okxStrategy := createSourceProcessStrategy(ctx, t, db, submit, domain.MarketSourceOKX)
	ca := testcert.New(t)
	naming := businessNaming()
	observer, err := nacos.New(ctx, naming)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := observer.Close(); err != nil {
			t.Error(err)
		}
	})
	binanceCfg := marketProcessConfig(t, ca, postgres, naming, binance.ID, "protect-binance", "99.99", "100.01")
	okxCfg := marketProcessConfig(t, ca, postgres, naming, okx.ID, "protect-okx", "100.01", "100.03")
	binancePID := startBusinessProcess(ctx, t, binanceCfg, observer)
	okxPID := startBusinessProcess(ctx, t, okxCfg, observer)
	execution := businessProcessConfig(t, ca, postgres, businessStream(ctx, t), naming, "execution")
	execution.Business.OperationTimeout = durationpb.New(time.Second)
	execution.Business.ExecutionLeaseTtl = durationpb.New(3 * time.Second)
	execution.Business.Risk = &configv1.RiskRuntime{AccountIds: []string{submit.Input.AccountID}}
	execution.Business.Simulator = &configv1.SimulatorScenarioRuntime{
		AccountIds:  []string{submit.Input.AccountID},
		CancelDelay: durationpb.New(100 * time.Millisecond),
	}
	startBusinessProcess(ctx, t, execution, observer)
	startBusinessProcess(ctx, t, strategyProcessConfig(t, ca, postgres, naming, submit.Input.AccountID, "source-protection"), observer)
	binanceStrategy := submit.Input.Authority.StrategyID
	waitStrategyProcessQuotes(ctx, t, db, binanceStrategy, 0)
	waitStrategyProcessQuotes(ctx, t, db, okxStrategy, 0)
	lostPID, lostStrategy, healthyStrategy, lostFeed, healthyFeed, restart := binancePID, binanceStrategy, okxStrategy, binance, okx, binanceCfg
	if lost == domain.MarketSourceOKX {
		lostPID, lostStrategy, healthyStrategy, lostFeed, healthyFeed, restart = okxPID, okxStrategy, binanceStrategy, okx, binance, okxCfg
	}
	process, err := os.FindProcess(lostPID)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	waitStrategySourceDrained(ctx, t, db, lostStrategy, lost)
	closedMax := strategyMaximumIntentID(ctx, t, db, lostStrategy)
	healthyMax := strategyMaximumIntentID(ctx, t, db, healthyStrategy)
	// The unaffected source must actually create newer quotes while the lost
	// source remains drained. Old resting quotes alone do not prove progress.
	waitHealthyStrategyProgress(ctx, t, db, healthyStrategy, healthyMax, healthyFeed.ID)
	if strategyMaximumIntentID(ctx, t, db, lostStrategy) != closedMax {
		t.Fatal("lost source created a replacement intent using the healthy source")
	}
	startBusinessProcess(ctx, t, restart, observer)
	current := waitStrategyProcessQuotes(ctx, t, db, lostStrategy, 0)
	if current.GetActiveMarketDataSource() != string(lost) {
		t.Fatal("recovery changed the persisted selected source")
	}
	ready := waitProcessRisk(ctx, t, db, lostStrategy, domain.RiskReasonReady)
	if !ready.AllowNewRisk || ready.GetMarketFeedId() != lostFeed.ID || strategyMaximumIntentID(ctx, t, db, lostStrategy) <= closedMax {
		t.Fatal("original source did not recover with its own current feed proof")
	}
	ids := []int64{binanceStrategy, okxStrategy}
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id IN ?", ids).UpdateColumn("desired_running", false).Error; err != nil {
		t.Fatal(err)
	}
	waitStrategyProcessesStopped(ctx, t, db, ids)
	core, _ := executionFixture(t, db)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "BTC", "10", "0")
}

func waitStrategySourceDrained(ctx context.Context, t *testing.T, db *gorm.DB, id int64, source domain.MarketSource) {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var row modelv1.StrategyInstance
		if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).First(&row, id).Error; err != nil {
			t.Fatal(err)
		}
		if row.GetActiveMarketDataSource() != string(source) || row.MarketDataSource != string(source) || !row.DesiredRunning {
			t.Fatal("source failure changed operator intent or source selection")
		}
		var open, active int64
		if err := db.WithContext(ctx).Model(&modelv1.OrderIntent{}).Where("strategy_instance_id = ? AND state NOT IN ?", id, []domain.OrderState{domain.OrderStateFilled, domain.OrderStateCanceled, domain.OrderStateRejected}).Count(&open).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.WithContext(ctx).Model(&modelv1.StrategyAction{}).Where("strategy_instance_id = ? AND state IN ?", id, []domain.StrategyActionState{domain.StrategyActionPending, domain.StrategyActionDispatching, domain.StrategyActionWaiting, domain.StrategyActionUnknown}).Count(&active).Error; err != nil {
			t.Fatal(err)
		}
		if row.State == string(domain.StrategyStateRecovering) && open == 0 && active == 0 {
			return
		}
		select {
		case <-ctx.Done():
			logStrategyRecoveryState(ctx, t, db, id)
			t.Fatal("lost selected source did not drain real orders and actions")
		case <-ticker.C:
		}
	}
}

func strategyMaximumIntentID(ctx context.Context, t *testing.T, db *gorm.DB, id int64) int64 {
	t.Helper()
	var maximum int64
	if err := db.WithContext(ctx).Model(&modelv1.OrderIntent{}).Where("strategy_instance_id = ?", id).Select("COALESCE(max(id), 0)").Scan(&maximum).Error; err != nil {
		t.Fatal(err)
	}
	return maximum
}

func waitHealthyStrategyProgress(ctx context.Context, t *testing.T, db *gorm.DB, id, after, feedID int64) {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var risk modelv1.RiskSnapshot
		if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ?", id).First(&risk).Error; err != nil {
			t.Fatal(err)
		}
		var live int64
		if err := db.WithContext(ctx).Model(&modelv1.OrderIntent{}).Where("strategy_instance_id = ? AND id > ? AND state = ?", id, after, domain.OrderStateLive).Count(&live).Error; err != nil {
			t.Fatal(err)
		}
		if live > 0 && risk.AllowNewRisk && risk.GetMarketFeedId() == feedID {
			return
		}
		select {
		case <-ctx.Done():
			logStrategyRecoveryState(ctx, t, db, id)
			t.Fatal("healthy source stopped actual quoting after another source failed")
		case <-ticker.C:
		}
	}
}
