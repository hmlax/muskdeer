//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/delivery/executiongrpc"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestStrategyClientTradesThroughOwnedRiskMarketAndSimulatorProcesses(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	portfolioFixture(ctx, t, db, submit)
	feed, publisher, _, _ := admissionSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	expireLeaseFixture(ctx, t, db, publisher.Key)
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
	marketCfg := marketProcessConfig(t, ca, postgres, naming, feed.ID, "typed-client", "99.99", "100.01")
	startBusinessProcess(ctx, t, marketCfg, observer)
	executionCfg := businessProcessConfig(t, ca, postgres, businessStream(ctx, t), naming, "execution")
	executionCfg.Business.OperationTimeout = durationpb.New(time.Second)
	executionCfg.Business.ExecutionLeaseTtl = durationpb.New(3 * time.Second)
	executionCfg.Business.Risk = &configv1.RiskRuntime{AccountIds: []string{submit.Input.AccountID}}
	executionCfg.Business.Simulator = &configv1.SimulatorScenarioRuntime{
		AccountIds:  []string{submit.Input.AccountID},
		CancelDelay: durationpb.New(100 * time.Millisecond),
	}
	startBusinessProcess(ctx, t, executionCfg, observer)
	routing, err := repository.NewExecutionRouting(db)
	if err != nil {
		t.Fatal(err)
	}
	factory, err := rpcclient.New(ctx, businessRPCConfig(ca.Issue(t, rpcsecurity.ServiceStrategy, false)), rpcsecurity.ServiceStrategy, observer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := factory.Close(); err != nil {
			t.Error(err)
		}
	})
	client, err := executiongrpc.NewStrategyClient(factory, routing)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Account(ctx, submit.Input.AccountID); err != nil {
		t.Fatal("typed authenticated read control failed before trading")
	}
	waitProcessRisk(ctx, t, db, submit.Input.Authority.StrategyID, domain.RiskReasonReady)
	input := submit.Input
	input.IdempotencyKey = "typed-client-buy"
	input.Price, input.Quantity = decimal.NewFromInt(99), decimal.NewFromInt(1)
	input.Deadline = time.Now().Add(time.Second)
	live, err := client.Submit(ctx, input)
	if err != nil || live.State != domain.OrderStateLive || live.Input.Price.String() != "99" {
		t.Fatalf("typed client failed to submit through actual owner: %+v, %v", live, err)
	}
	found, err := client.FindOrder(ctx, input.AccountID, input.IdempotencyKey)
	if err != nil || found.ID != live.ID || found.ClientOrderID != live.ClientOrderID {
		t.Fatalf("typed stable-key order recovery changed identity: %+v, %v", found, err)
	}
	orders, next, err := client.Orders(ctx, domain.ExecutionFilter{
		AccountID:    input.AccountID,
		StrategyID:   input.Authority.StrategyID,
		InstrumentID: input.InstrumentID,
		Limit:        domain.MaximumExecutionPage,
		OpenOnly:     true,
	})
	if err != nil || len(orders) != 1 || orders[0].ID != live.ID || next != 0 {
		t.Fatalf("typed complete observation failed: %+v, %d, %v", orders, next, err)
	}
	account, err := client.Account(ctx, input.AccountID)
	if err != nil || len(account.Balances) != 2 || account.Balances[1].Asset != "USDT" || account.Balances[1].Frozen.String() != "99.099" {
		t.Fatalf("typed core funds observation changed precision: %+v, %v", account, err)
	}
	control := cancelInputFixture(ctx, submit, live.ClientOrderID, "typed-client-cancel")
	control.Deadline = time.Now().Add(time.Second)
	pending, err := client.Cancel(ctx, control)
	if err != nil || pending.Request.State != domain.CancellationPending {
		t.Fatalf("typed cancellation did not retain pending state: %+v, %v", pending, err)
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		result, err := client.FindCancellation(ctx, control.AccountID, control.IdempotencyKey)
		if err != nil {
			t.Fatal(err)
		}
		if result.Request.State == domain.CancellationConfirmed && result.Order.State == domain.OrderStateCanceled {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("typed cancellation recovery never observed simulator terminal state")
		case <-ticker.C:
		}
	}
	assertReservedBudget(ctx, t, db, input.AccountID, "0")
	core, _ := executionFixture(t, db)
	assertCoreBalance(ctx, t, core, input.AccountID, "USDT", "1000", "0")
	assertGatewayOrderCount(ctx, t, db, 1)
}
