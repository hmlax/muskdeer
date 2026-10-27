//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/types/known/durationpb"

	executionv1 "github.com/hmlax/muskdeer/api/execution/v1"
	"github.com/hmlax/muskdeer/deer/nacos"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestSimulatorCancelScenarioUsesOriginalCommandAgeAndPreservesRacingFill(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	orders := intentServiceFixture(t, intents, core)
	live, err := orders.Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	cancels := cancellationServiceFixture(t, db, intents, core)
	pending, err := cancels.Cancel(ctx, cancelInputFixture(ctx, submit, live.ClientOrderID, "scenario-cancel"), submit.ExecutionAuthority)
	if err != nil || pending.Request.State != domain.CancellationPending {
		t.Fatalf("scenario cancellation was not pending: %+v, %v", pending, err)
	}
	candidates, err := repository.NewSimulatorScenarios(db)
	if err != nil {
		t.Fatal(err)
	}
	if client, err := candidates.DueCancellation(ctx, submit.Input.AccountID, 5*time.Second); err != nil || client != "" {
		t.Fatalf("scenario confirmed before explicit delay: %s, %v", client, err)
	}
	if client, err := candidates.DueCancellation(ctx, "another-account", time.Millisecond); err != nil || client != "" {
		t.Fatal("scenario crossed its configured account scope")
	}
	// Wait for the original cancellation command, then fill immediately before
	// scanning. A fill's update_at must not restart the cancellation delay.
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-timer.C:
	}
	if _, err := core.Fill(ctx, domain.CoreFillRequest{
		AccountID:           submit.Input.AccountID,
		ClientOrderID:       live.ClientOrderID,
		TradeID:             "scenario-cancel-racing-fill",
		CounterpartySubject: "external-scenario",
		Price:               decimal.NewFromInt(100),
		Quantity:            decimal.NewFromInt(1),
	}); err != nil {
		t.Fatal(err)
	}
	client, err := candidates.DueCancellation(ctx, submit.Input.AccountID, 100*time.Millisecond)
	if err != nil || client != live.ClientOrderID {
		t.Fatalf("fill restarted cancellation delay or lost pending order: %s, %v", client, err)
	}
	closed, err := core.ConfirmCancel(ctx, submit.Input.AccountID, client)
	if err != nil || closed.State != domain.OrderStateCanceled || !closed.FilledQuantity.Equal(decimal.NewFromInt(1)) {
		t.Fatalf("scenario lost racing fill: %+v, %v", closed, err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
	observed, err := cancels.Recover(ctx, submit.Input.AccountID, pending.Request.ID)
	if err != nil || observed.Request.State != domain.CancellationConfirmed || !observed.Order.FilledQuantity.Equal(decimal.NewFromInt(1)) {
		t.Fatalf("OMS did not observe scenario terminal state: %+v, %v", observed, err)
	}
	if next, err := candidates.DueCancellation(ctx, submit.Input.AccountID, time.Millisecond); err != nil || next != "" {
		t.Fatal("terminal cancellation remained eligible")
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "899.9", "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "BTC", "11", "0")
}

func TestExecutionProcessConfirmsExplicitSimulatorCancellation(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	cancels := cancellationServiceFixture(t, db, intents, core)
	pending, err := cancels.Cancel(ctx, cancelInputFixture(ctx, submit, live.ClientOrderID, "process-scenario-cancel"), submit.ExecutionAuthority)
	if err != nil || pending.Request.State != domain.CancellationPending {
		t.Fatalf("process scenario never entered pending: %+v, %v", pending, err)
	}
	expireLeaseFixture(ctx, t, db, submit.ExecutionAuthority.Key)
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
	cfg := businessProcessConfig(t, ca, postgres, businessStream(ctx, t), naming, "execution")
	cfg.Business.OperationTimeout = durationpb.New(200 * time.Millisecond)
	cfg.Business.ExecutionLeaseTtl = durationpb.New(3 * time.Second)
	cfg.Business.Simulator = &configv1.SimulatorScenarioRuntime{
		AccountIds:  []string{submit.Input.AccountID},
		CancelDelay: durationpb.New(time.Millisecond),
	}
	startBusinessProcess(ctx, t, cfg, observer)
	waitSimulatorCancellation(ctx, t, core, submit.Input.AccountID, live.ClientOrderID)
	// Venue terminal state alone does not pretend OMS has observed it.
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
	factory, err := rpcclient.New(ctx, businessRPCConfig(ca.Issue(t, rpcsecurity.ServiceStrategy, false)), rpcsecurity.ServiceStrategy, observer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := factory.Close(); err != nil {
			t.Error(err)
		}
	})
	connection, err := factory.Client(rpcclient.Target{
		Service:    rpcsecurity.ServiceExecution,
		InstanceID: cfg.InstanceId,
	})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := executionv1.NewExecutionClient(connection).FindCancellation(ctx, &executionv1.FindCancellationRequest{
		AccountId:      submit.Input.AccountID,
		CancellationId: pending.Request.ID,
	})
	if err != nil || reply.GetOrder().GetState() != commonv1.OrderState_ORDER_STATE_CANCELED || reply.GetCancellation().GetState() != commonv1.CancellationState_CANCELLATION_STATE_CONFIRMED {
		t.Fatalf("process terminal observation failed: %v, %v", reply, err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
}

func waitSimulatorCancellation(ctx context.Context, t *testing.T, core domain.Simulator, account, client string) {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		order, err := core.Find(ctx, account, client)
		if err != nil {
			t.Fatal(err)
		}
		if order.State == domain.OrderStateCanceled {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("simulator process did not confirm the pending cancellation")
		case <-ticker.C:
		}
	}
}
