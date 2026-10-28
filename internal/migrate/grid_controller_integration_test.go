//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/usecase"
)

func TestGridControllerMaintainsFixedLevelsAndWaitsForTerminalStop(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	controller, execution, market, submit := gridControllerFixture(ctx, t, db)
	orders := waitGridControllerQuotes(ctx, t, controller, execution, market, submit)
	submits := execution.submits
	runGridController(ctx, t, controller, market, "100", 4)
	if execution.submits != submits {
		t.Fatal("unchanged fixed grid levels were needlessly replaced")
	}
	stopGridController(ctx, t, db, controller, execution, market, submit, orders)
}

func TestGridControllerDrainsOutsideRangeAndReturnsOnlyOnSelectedReference(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	controller, execution, market, submit := gridControllerFixture(ctx, t, db)
	orders := waitGridControllerQuotes(ctx, t, controller, execution, market, submit)
	submits, cancels := execution.submits, execution.cancels
	runGridController(ctx, t, controller, market, "110", 12)
	if execution.submits != submits || execution.cancels-cancels != len(orders) {
		t.Fatal("out-of-range grid added risk or omitted controlled cancels")
	}
	for _, order := range orders {
		if _, err := execution.core.ConfirmCancel(ctx, order.Input.AccountID, order.ClientOrderID); err != nil {
			t.Fatal(err)
		}
	}
	runGridController(ctx, t, controller, market, "110", 8)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	if execution.submits != submits {
		t.Fatal("outside reference silently restarted grid submissions")
	}
	orders = waitGridControllerQuotes(ctx, t, controller, execution, market, submit)
	if execution.submits < submits+len(orders) {
		t.Fatal("selected reference recovery did not restore fixed funded levels")
	}
	stopGridController(ctx, t, db, controller, execution, market, submit, orders)
}

func gridControllerFixture(ctx context.Context, t *testing.T, db *gorm.DB) (*usecase.StrategyController, *controllerExecution, *controllerMarket, domain.GatewaySubmit) {
	t.Helper()
	_, submit := admissionFixture(ctx, t, db)
	portfolioFixture(ctx, t, db, submit)
	instance := identityInstance(submit.Input.InstrumentID, "grid-controller", domain.StrategyTemplateGrid, domain.MarketSourceBinance)
	instance.AccountId = submit.Input.AccountID
	instance.State = string(domain.StrategyStateRunning)
	instance.DesiredRunning = true
	instance.ActiveConfigVersion = proto.Int64(1)
	instance.ActiveMarketDataSource = proto.String(string(domain.MarketSourceBinance))
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Create(instance).Error; err != nil {
			return err
		}
		parameters := storedParameterFixture(domain.StrategyTemplateGrid)
		parameters.InstanceId = instance.Id
		parameters.GridParameters = &commonv1.GridParameters{
			LowerPrice:       "98",
			UpperPrice:       "102",
			GridCount:        5,
			TotalBudget:      "50",
			PerOrderNotional: "10",
			InventoryLimit:   "20",
			MaxMarketAgeMs:   500,
		}
		return tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Create(parameters).Error
	}); err != nil {
		t.Fatal(err)
	}
	lease := acquireGatewayLease(ctx, t, db, domain.LeaseKey{
		Kind:       domain.LeaseKindStrategy,
		StrategyID: instance.Id,
	})
	submit.Input.Authority = domain.StrategyAuthority{
		StrategyID:    instance.Id,
		ConfigVersion: 1,
		OwnerID:       lease.OwnerID,
		OwnerEpoch:    lease.Epoch,
	}
	seedAdmissionStrategy(ctx, t, db, submit.Input)
	controller, execution, market := strategyControllerFixture(ctx, t, db, submit)
	return controller, execution, market, submit
}

func runGridController(ctx context.Context, t *testing.T, controller *usecase.StrategyController, market *controllerMarket, midpoint string, ticks int) {
	t.Helper()
	for range ticks {
		strategyControllerTickAt(ctx, t, controller, market, midpoint)
		time.Sleep(110 * time.Millisecond)
	}
}

func assertGridControllerQuotes(ctx context.Context, t *testing.T, execution *controllerExecution, submit domain.GatewaySubmit) []domain.IntentSnapshot {
	t.Helper()
	orders := gridControllerOrders(ctx, t, execution, submit)
	if !gridControllerQuotesLive(orders) {
		t.Fatalf("fixed grid did not produce four actual live funded levels: count=%d", len(orders))
	}
	assertGridPrices(t, orders)
	return orders
}

func gridControllerOrders(ctx context.Context, t *testing.T, execution *controllerExecution, submit domain.GatewaySubmit) []domain.IntentSnapshot {
	t.Helper()
	orders, _, err := execution.Orders(ctx, domain.ExecutionFilter{
		AccountID:    submit.Input.AccountID,
		StrategyID:   submit.Input.Authority.StrategyID,
		InstrumentID: submit.Input.InstrumentID,
		Limit:        100,
		OpenOnly:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return orders
}

func gridControllerQuotesLive(orders []domain.IntentSnapshot) bool {
	if len(orders) != 4 {
		return false
	}
	for _, order := range orders {
		if order.State != domain.OrderStateLive || order.CancelState != domain.CancelStateNone || order.CoreOrderID <= 0 {
			return false
		}
	}
	return true
}

func waitGridControllerQuotes(ctx context.Context, t *testing.T, controller *usecase.StrategyController, execution *controllerExecution, market *controllerMarket, submit domain.GatewaySubmit) []domain.IntentSnapshot {
	t.Helper()
	ticks := time.NewTicker(110 * time.Millisecond)
	defer ticks.Stop()
	for {
		strategyControllerTick(ctx, t, controller, market)
		orders := gridControllerOrders(ctx, t, execution, submit)
		if gridControllerQuotesLive(orders) {
			return assertGridControllerQuotes(ctx, t, execution, submit)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("grid did not converge within its original scenario deadline: count=%d error=%v", len(orders), ctx.Err())
		case <-ticks.C:
		}
	}
}

func assertGridPrices(t *testing.T, orders []domain.IntentSnapshot) {
	t.Helper()
	for _, expected := range []struct {
		price int64
		side  domain.Side
	}{
		{
			price: 98,
			side:  domain.SideBuy,
		},
		{
			price: 99,
			side:  domain.SideBuy,
		},
		{
			price: 101,
			side:  domain.SideSell,
		},
		{
			price: 102,
			side:  domain.SideSell,
		},
	} {
		matches := 0
		for _, order := range orders {
			if order.Input.Price.Equal(decimal.NewFromInt(expected.price)) && order.Input.Side == expected.side {
				matches++
			}
		}
		if matches != 1 {
			t.Fatal("grid level moved, disappeared or duplicated")
		}
	}
}

func stopGridController(ctx context.Context, t *testing.T, db *gorm.DB, controller *usecase.StrategyController, execution *controllerExecution, market *controllerMarket, submit domain.GatewaySubmit, orders []domain.IntentSnapshot) {
	t.Helper()
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", submit.Input.Authority.StrategyID).UpdateColumn("desired_running", false).Error; err != nil {
		t.Fatal(err)
	}
	runGridController(ctx, t, controller, market, "100", 12)
	var instance modelv1.StrategyInstance
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).First(&instance, submit.Input.Authority.StrategyID).Error; err != nil {
		t.Fatal(err)
	}
	if instance.State != string(domain.StrategyStateStopping) {
		t.Fatal("cancel acceptance incorrectly completed a grid stop")
	}
	for _, order := range orders {
		if _, err := execution.core.ConfirmCancel(ctx, order.Input.AccountID, order.ClientOrderID); err != nil {
			t.Fatal(err)
		}
	}
	runGridController(ctx, t, controller, market, "100", 8)
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).First(&instance, instance.Id).Error; err != nil || instance.State != string(domain.StrategyStateStopped) {
		t.Fatal("confirmed terminal grid orders did not complete stop")
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
	assertCoreBalance(ctx, t, execution.core, submit.Input.AccountID, "USDT", "1000", "0")
	assertCoreBalance(ctx, t, execution.core, submit.Input.AccountID, "BTC", "10", "0")
}
