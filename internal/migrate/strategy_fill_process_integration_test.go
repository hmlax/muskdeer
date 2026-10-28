//go:build integration

package migrate

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/types/known/durationpb"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestStrategyProcessesPartialFillsProjectFeesAndStopWithRealFunds(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, _ := executionFixture(t, db)
	portfolio := portfolioFixture(ctx, t, db, submit)
	startStrategyFillProcesses(ctx, t, db, postgres, submit)
	waitStrategyProcessQuotes(ctx, t, db, submit.Input.Authority.StrategyID, 0)
	started := time.Now()
	fills := driveStrategyPartialFills(ctx, t, db, core, submit, 6*time.Second)
	if time.Since(started) < 6*time.Second || fills < 2 {
		t.Fatal("partial-fill scenario did not run its real duration or both sides")
	}
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", submit.Input.Authority.StrategyID).UpdateColumn("desired_running", false).Error; err != nil {
		t.Fatal(err)
	}
	waitStrategyProcessesStopped(ctx, t, db, []int64{submit.Input.Authority.StrategyID})
	snapshot := waitStrategyFillProjection(ctx, t, db, portfolio, submit.Input.AccountID, fills)
	assertStrategyFillAccounting(ctx, t, db, core, portfolio, submit, snapshot, fills)
}

func startStrategyFillProcesses(ctx context.Context, t *testing.T, db *gorm.DB, postgres *configv1.Postgres, submit domain.GatewaySubmit) {
	t.Helper()
	feed, publisher, _, _ := admissionSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	expireLeaseFixture(ctx, t, db, publisher.Key)
	expireLeaseFixture(ctx, t, db, submit.ExecutionAuthority.Key)
	expireLeaseFixture(ctx, t, db, lifecycleToken(submit.Input.Authority).Key)
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
	stream := businessStream(ctx, t)
	startBusinessProcess(ctx, t, marketProcessConfig(t, ca, postgres, naming, feed.ID, "fill-scenario", "99.99", "100.01"), observer)
	execution := businessProcessConfig(t, ca, postgres, stream, naming, "execution")
	execution.Business.OperationTimeout = durationpb.New(time.Second)
	execution.Business.ExecutionLeaseTtl = durationpb.New(3 * time.Second)
	execution.Business.Risk = &configv1.RiskRuntime{AccountIds: []string{submit.Input.AccountID}}
	execution.Business.Simulator = &configv1.SimulatorScenarioRuntime{
		AccountIds:  []string{submit.Input.AccountID},
		CancelDelay: durationpb.New(100 * time.Millisecond),
	}
	startBusinessProcess(ctx, t, execution, observer)
	startBusinessProcess(ctx, t, businessProcessConfig(t, ca, postgres, stream, naming, "portfolio"), observer)
	startBusinessProcess(ctx, t, strategyProcessConfig(t, ca, postgres, naming, submit.Input.AccountID, "fills"), observer)
}

func driveStrategyPartialFills(ctx context.Context, t *testing.T, db *gorm.DB, core domain.Simulator, submit domain.GatewaySubmit, duration time.Duration) int64 {
	t.Helper()
	return driveStrategyFillsObserved(ctx, t, db, core, submit, duration, nil)
}

func driveStrategyFillsObserved(ctx context.Context, t *testing.T, db *gorm.DB, core domain.Simulator, submit domain.GatewaySubmit, duration time.Duration, observe func(time.Duration, int64)) int64 {
	t.Helper()
	started := time.Now()
	until, next := started.Add(duration), started
	ticks := time.NewTicker(100 * time.Millisecond)
	defer ticks.Stop()
	var fills int64
	side := domain.SideBuy
	for time.Now().Before(until) {
		if observe != nil {
			observe(time.Since(started), fills)
		}
		if !time.Now().Before(next) && fillStrategySide(ctx, t, db, core, submit, side) {
			fills++
			next = time.Now().Add(2 * time.Second)
			if side == domain.SideBuy {
				side = domain.SideSell
			} else {
				side = domain.SideBuy
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticks.C:
		}
	}
	return fills
}

func fillStrategySide(ctx context.Context, t *testing.T, db *gorm.DB, core domain.Simulator, submit domain.GatewaySubmit, side domain.Side) bool {
	t.Helper()
	call, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var row modelv1.SimulatorOrder
	err := db.WithContext(call).Model(&modelv1.SimulatorOrder{}).
		Where("account_id = ? AND strategy_instance_id = ? AND side = ? AND state IN ? AND cancel_state = ? AND delete_at IS NULL", submit.Input.AccountID, submit.Input.Authority.StrategyID, side, []domain.OrderState{domain.OrderStateLive, domain.OrderStatePartiallyFilled}, domain.CancelStateNone).
		Order("id DESC").Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	price := decimal.RequireFromString(row.Price)
	remaining := decimal.RequireFromString(row.Quantity).Sub(decimal.RequireFromString(row.FilledQuantity))
	quantity := decimal.Min(remaining, decimal.RequireFromString("0.01"))
	request := domain.CoreFillRequest{
		AccountID:           row.AccountId,
		ClientOrderID:       row.ClientOrderId,
		TradeID:             fmt.Sprintf("scenario-%d-%s", row.Id, row.FilledQuantity),
		CounterpartySubject: "external-scenario-liquidity",
		Price:               price,
		Quantity:            quantity,
	}
	filled, err := core.Fill(call, request)
	if errors.Is(err, domain.ErrInvalidOrder) {
		current, queryErr := core.Find(call, row.AccountId, row.ClientOrderId)
		if queryErr == nil && (current.State == domain.OrderStateCanceled || current.State == domain.OrderStateFilled) {
			// A confirmed terminal race has no new fill effect. The driver
			// waits for a different, currently live order; it never resends it.
			return false
		}
	}
	if err != nil {
		t.Fatalf("actual external partial fill failed: %v", err)
	}
	duplicate, err := core.Fill(call, request)
	if err != nil || duplicate.ID != filled.ID || !duplicate.FeeAmount.Equal(filled.FeeAmount) {
		t.Fatal("repeated natural fill identity changed core effects")
	}
	if !quantity.LessThan(remaining) {
		t.Fatal("scenario did not produce an actual partial fill")
	}
	return true
}

func waitStrategyFillProjection(ctx context.Context, t *testing.T, db *gorm.DB, portfolio domain.PortfolioRepository, account string, fills int64) domain.PortfolioSnapshot {
	t.Helper()
	proof, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticks := time.NewTicker(20 * time.Millisecond)
	defer ticks.Stop()
	for {
		snapshot, err := portfolio.Get(proof, account)
		if err != nil {
			t.Fatal(err)
		}
		var projected, pending int64
		if err := db.WithContext(proof).Model(&modelv1.PortfolioFill{}).Where("account_id = ?", account).Count(&projected).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.WithContext(proof).Model(&modelv1.OutboxEvent{}).Where("state <> ?", domain.OutboxStatePublished).Count(&pending).Error; err != nil {
			t.Fatal(err)
		}
		if snapshot.Reconciled && projected == fills && pending == 0 {
			return snapshot
		}
		select {
		case <-proof.Done():
			t.Fatal("actual strategy fill projection did not converge")
		case <-ticks.C:
		}
	}
}

func assertStrategyFillAccounting(ctx context.Context, t *testing.T, db *gorm.DB, core domain.Simulator, portfolio domain.PortfolioRepository, submit domain.GatewaySubmit, snapshot domain.PortfolioSnapshot, fills int64) {
	t.Helper()
	assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorFill{}, fills)
	assertPortfolioRowCount(ctx, t, db, &modelv1.PortfolioFill{}, fills)
	assertPortfolioRowCount(ctx, t, db, &modelv1.ConsumerInbox{}, fills)
	intents := db.WithContext(ctx).Model(&modelv1.OrderIntent{}).Select("client_order_id").Where("account_id = ?", submit.Input.AccountID)
	var unexplained int64
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Where("account_id = ? AND client_order_id NOT IN (?)", submit.Input.AccountID, intents).Count(&unexplained).Error; err != nil || unexplained != 0 {
		t.Fatal("core contains an order without its prior durable intent")
	}
	if !snapshot.Values.FeesQuote.IsPositive() {
		t.Fatal("actual strategy fills omitted quote fees")
	}
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "BTC", snapshot.Values.Quantity.String(), "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", snapshot.Values.CashQuote.String(), "0")
	var facts []*modelv1.PortfolioFill
	if err := db.WithContext(ctx).Model(&modelv1.PortfolioFill{}).Order("source_sequence ASC").Find(&facts).Error; err != nil {
		t.Fatal(err)
	}
	quantity, cash, fees := decimal.NewFromInt(10), decimal.NewFromInt(1000), decimal.Zero
	buy, sell := false, false
	for _, fact := range facts {
		amount := decimal.RequireFromString(fact.Quantity)
		notional := decimal.RequireFromString(fact.QuoteNotional)
		fee := decimal.RequireFromString(fact.FeeAmount)
		fees = fees.Add(fee)
		cash = cash.Sub(fee)
		switch domain.Side(fact.Side) {
		case domain.SideBuy:
			buy = true
			quantity, cash = quantity.Add(amount), cash.Sub(notional)
		case domain.SideSell:
			sell = true
			quantity, cash = quantity.Sub(amount), cash.Add(notional)
		default:
			t.Fatal("unknown fill side entered accounting")
		}
	}
	if !buy || !sell || !quantity.Equal(snapshot.Values.Quantity) || !cash.Equal(snapshot.Values.CashQuote) || !fees.Equal(snapshot.Values.FeesQuote) {
		t.Fatal("immutable fills do not explain exact core funds and fees")
	}
	rebuilt, err := portfolio.Rebuild(ctx, submit.Input.AccountID)
	if err != nil || !rebuilt.Reconciled || !sameScenarioPortfolio(rebuilt.Values, snapshot.Values) {
		t.Fatal("strategy fill cost/PnL projection did not rebuild exactly")
	}
	t.Logf("actual partial fills=%d base=%s cash=%s fees=%s cost=%s realized_pnl=%s", fills, quantity, cash, fees, snapshot.Values.CostQuote, snapshot.Values.RealizedPnlQuote)
}

func sameScenarioPortfolio(a, b domain.PortfolioValues) bool {
	return a.Quantity.Equal(b.Quantity) && a.CashQuote.Equal(b.CashQuote) && a.CostQuote.Equal(b.CostQuote) &&
		a.FeesQuote.Equal(b.FeesQuote) && a.RealizedPnlQuote.Equal(b.RealizedPnlQuote)
}
