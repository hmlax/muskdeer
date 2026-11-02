//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestOperatorProcessOwnsMarketExecutionStrategyAndPortfolio(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	portfolioFixture(ctx, t, db, submit)
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
	cfg := businessProcessConfig(t, ca, postgres, businessStream(ctx, t), naming, "operator")
	cfg.GrpcSecurity.AllowedPeerServices = []string{rpcsecurity.ServiceAdmin, rpcsecurity.ServiceOperator}
	cfg.GrpcClients = businessRPCConfig(ca.Issue(t, rpcsecurity.ServiceOperator, false))
	cfg.GrpcClients.AllowedServices = []string{rpcsecurity.ServiceOperator}
	cfg.Business.OperationTimeout = durationpb.New(time.Second)
	cfg.Business.ExecutionLeaseTtl = durationpb.New(3 * time.Second)
	cfg.Business.Strategy = &configv1.StrategyRuntime{
		AccountIds:        []string{submit.Input.AccountID},
		LeaseTtl:          durationpb.New(3 * time.Second),
		ManagementEnabled: true,
	}
	cfg.Business.Market = marketProcessConfig(t, ca, postgres, naming, feed.ID, "operator", "99.99", "100.01").Business.Market
	cfg.Business.Risk = &configv1.RiskRuntime{AccountIds: []string{submit.Input.AccountID}}
	cfg.Business.Simulator = &configv1.SimulatorScenarioRuntime{
		AccountIds:  []string{submit.Input.AccountID},
		CancelDelay: durationpb.New(100 * time.Millisecond),
	}
	cfg.Business.PortfolioConsumer = "portfolio_business"
	startBusinessProcess(ctx, t, cfg, observer)
	instance := waitStrategyProcessQuotes(ctx, t, db, submit.Input.Authority.StrategyID, 0)
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", instance.Id).UpdateColumn("desired_running", false).Error; err != nil {
		t.Fatal(err)
	}
	waitStrategyProcessesStopped(ctx, t, db, []int64{instance.Id})
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	core, _ := executionFixture(t, db)
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "BTC", "10", "0")
}
