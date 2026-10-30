//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/simulator"
)

func TestAdminCancellationControllerRecoversLostResponseAcrossTakeover(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	controller, execution, market := strategyControllerFixture(ctx, t, db, submit)
	// Funding was authorized by the original owner before the controller takes
	// over. Renew that owner only to seed the existing order, then expire it.
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	submit = acquireStrategyForAdminCancel(ctx, t, leases, submit)
	submit.Input.Deadline = time.Now().Add(time.Second)
	order, err := execution.intents.Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	control, err := repository.NewAdminOrderCancellations(db)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := control.Request(ctx, actor.Token, actor.CSRF, adminCancelRequest(order))
	if err != nil {
		t.Fatal(err)
	}
	lost, err := simulator.WithFaults(execution.core, simulator.FaultPlan{
		Submit: simulator.ResponseNormal,
		Cancel: simulator.ResponseLost,
	})
	if err != nil {
		t.Fatal(err)
	}
	execution.requests = cancellationServiceFixture(t, db, execution.store, lost)
	expireLeaseFixture(ctx, t, db, domain.LeaseKey{
		Kind:       domain.LeaseKindStrategy,
		StrategyID: receipt.StrategyID,
	})
	for range 4 {
		strategyControllerTick(ctx, t, controller, market)
	}
	assertAdminCancellationPending(ctx, t, control, actor, receipt)
	if execution.cancels != 1 || execution.submits != 0 {
		t.Fatal("uncertain cancellation was resent or introduced a new order")
	}
	if _, err := execution.core.Fill(ctx, domain.CoreFillRequest{
		AccountID:           submit.Input.AccountID,
		ClientOrderID:       order.ClientOrderID,
		TradeID:             "admin-cancel-in-flight-fill",
		CounterpartySubject: "external-liquidity",
		Price:               decimal.NewFromInt(100),
		Quantity:            decimal.NewFromInt(1),
	}); err != nil {
		t.Fatal(err)
	}
	expireLeaseFixture(ctx, t, db, domain.LeaseKey{
		Kind:       domain.LeaseKindStrategy,
		StrategyID: receipt.StrategyID,
	})
	other := strategyControllerWith(ctx, t, db, submit, leases, execution, market)
	for range 3 {
		strategyControllerTick(ctx, t, other, market)
	}
	assertAdminCancellationPending(ctx, t, control, actor, receipt)
	if _, err := execution.core.ConfirmCancel(ctx, submit.Input.AccountID, order.ClientOrderID); err != nil {
		t.Fatal(err)
	}
	for range 6 {
		strategyControllerTick(ctx, t, other, market)
		read, err := control.Receipt(ctx, actor.Token, receipt.StrategyID, receipt.ID)
		if err != nil {
			t.Fatal(err)
		}
		if read.State == domain.AdminCancellationSucceeded {
			assertAdminCancellationRuntimeResult(ctx, t, control, actor, read, execution)
			finalOrder, err := execution.store.ByClient(ctx, submit.Input.AccountID, order.ClientOrderID)
			if err != nil || finalOrder.FilledQuantity.String() != "1" || !finalOrder.ReservedQuoteNotional.IsZero() {
				t.Fatal("terminal cancellation lost racing fill or retained its reservation", err)
			}
			assertCoreBalance(ctx, t, execution.core, submit.Input.AccountID, "USDT", "899.9", "0")
			assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
			assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyAction{}, 1)
			assertPortfolioRowCount(ctx, t, db, &modelv1.CancellationRequest{}, 1)
			assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorFill{}, 1)
			return
		}
	}
	t.Fatal("replacement controller did not complete original management cancellation")
}

func acquireStrategyForAdminCancel(ctx context.Context, t *testing.T, leases domain.LeaseRepository, submit domain.GatewaySubmit) domain.GatewaySubmit {
	t.Helper()
	token, err := leases.Acquire(ctx, domain.LeaseKey{
		Kind:       domain.LeaseKindStrategy,
		StrategyID: submit.Input.Authority.StrategyID,
	}, submit.Input.Authority.OwnerID, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	submit.Input.Authority.OwnerEpoch = token.Epoch
	return submit
}

func assertAdminCancellationRuntimeResult(ctx context.Context, t *testing.T, control domain.AdminOrderCancellations, actor domain.AdminLogin, done domain.AdminCancellationReceipt, execution *controllerExecution) {
	t.Helper()
	if done.ObservedOrderState != domain.OrderStateCanceled || done.CompletedAt.IsZero() || execution.cancels != 1 || execution.submits != 0 {
		t.Fatal("terminal receipt lost order fact or sent a duplicate command")
	}
	if again, err := control.Receipt(ctx, actor.Token, done.StrategyID, done.ID); err != nil || again != done {
		t.Fatal("terminal cancellation history changed", err)
	}
}
