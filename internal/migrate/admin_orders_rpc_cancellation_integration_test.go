//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestAdminOrderRPCDoesNotReleaseFundsWhenCoreCancellationPrecedesOMS(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	cancels := cancellationServiceFixture(t, db, intents, core)
	pending, err := cancels.Cancel(ctx, cancelInputFixture(ctx, submit, live.ClientOrderID, "admin-order-rpc-pending-cancel"), submit.ExecutionAuthority)
	if err != nil || pending.Request.State != domain.CancellationPending {
		t.Fatal(err)
	}
	ca := testcert.New(t)
	naming := businessNaming()
	discovery, err := nacos.New(ctx, naming)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := discovery.Close(); err != nil {
			t.Error(err)
		}
	})
	strategy := businessProcessConfig(t, ca, postgres, nil, naming, "strategy")
	strategy.Business.Strategy = &configv1.StrategyRuntime{ManagementEnabled: true}
	startBusinessProcess(ctx, t, strategy, discovery)
	reader := strategyv1.NewStrategyOrderQueryClient(adminStrategyProcessConnection(ctx, t, ca, discovery, rpcsecurity.ServiceAPI))
	request := &strategyv1.GetStrategyOrderRequest{
		Session:    &strategyv1.AdminSession{Token: actor.Token},
		StrategyId: submit.Input.Authority.StrategyID,
		OrderId:    live.ID,
	}
	before, err := reader.GetStrategyOrder(ctx, request)
	if err != nil || before.Order.CancelState != string(domain.CancelStatePending) || before.Order.ReservedNotional != "200" || before.Order.Core.FrozenAmount != "200.2" {
		t.Fatal("actual pending cancellation lost reserved funds", err)
	}
	if _, err := core.ConfirmCancel(ctx, submit.Input.AccountID, live.ClientOrderID); err != nil {
		t.Fatal(err)
	}
	gap, err := reader.GetStrategyOrder(ctx, request)
	if err != nil || gap.Order.State != string(domain.OrderStateLive) || gap.Order.CancelState != string(domain.CancelStatePending) || gap.Order.ReservedNotional != "200" || gap.Order.Core.State != string(domain.OrderStateCanceled) || gap.Order.Core.CancelState != string(domain.CancelStateConfirmed) || gap.Order.Core.FrozenAmount != "0" {
		t.Fatal("actual RPC reconciled core cancellation or released OMS reserves", err)
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
	if result, err := cancels.Recover(ctx, submit.Input.AccountID, pending.Request.ID); err != nil || result.Request.State != domain.CancellationConfirmed {
		t.Fatal(err)
	}
	after, err := reader.GetStrategyOrder(ctx, request)
	if err != nil || after.Order.State != string(domain.OrderStateCanceled) || after.Order.ReservedNotional != "0" || after.Order.ConfirmedCoreRevision != after.Order.Core.Revision {
		t.Fatal("actual RPC lost later authoritative OMS confirmation", err)
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
	assertGatewayOrderCount(ctx, t, db, 1)
}
