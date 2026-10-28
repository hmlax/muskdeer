//go:build integration

package migrate

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestStrategyCommandsDiscoverTakeOverQuoteAndCompleteStop(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
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
	marketPID := startBusinessProcess(ctx, t, marketProcessConfig(t, ca, postgres, naming, feed.ID, "strategy-process", "99.99", "100.01"), observer)
	execution := businessProcessConfig(t, ca, postgres, businessStream(ctx, t), naming, "execution")
	execution.Business.OperationTimeout = durationpb.New(time.Second)
	execution.Business.ExecutionLeaseTtl = durationpb.New(3 * time.Second)
	execution.Business.Risk = &configv1.RiskRuntime{AccountIds: []string{submit.Input.AccountID}}
	execution.Business.Simulator = &configv1.SimulatorScenarioRuntime{
		AccountIds:  []string{submit.Input.AccountID},
		CancelDelay: durationpb.New(100 * time.Millisecond),
	}
	executionPID := startBusinessProcess(ctx, t, execution, observer)
	first := strategyProcessConfig(t, ca, postgres, naming, submit.Input.AccountID, "a")
	firstPID := startBusinessProcess(ctx, t, first, observer)
	prior := waitStrategyProcessQuotes(ctx, t, db, submit.Input.Authority.StrategyID, 0)
	second := strategyProcessConfig(t, ca, postgres, naming, submit.Input.AccountID, "b")
	secondPID := startBusinessProcess(ctx, t, second, observer)
	if firstPID == secondPID || firstPID == executionPID || firstPID == marketPID || firstPID == os.Getpid() {
		t.Fatal("strategy, standby, execution and market did not run as separate processes")
	}
	var still modelv1.StrategyInstance
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).First(&still, prior.Id).Error; err != nil || still.RuntimeOwnerId != prior.RuntimeOwnerId {
		t.Fatal("standby startup stole a valid instance lease")
	}
	process, err := os.FindProcess(firstPID)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	replaced := waitStrategyProcessQuotes(ctx, t, db, prior.Id, prior.RuntimeOwnerEpoch)
	if replaced.RuntimeOwnerId == prior.RuntimeOwnerId {
		t.Fatal("replacement reused the old process incarnation")
	}
	// A new persisted instance is discovered after both processes started;
	// constructing it does not require a worker/configuration restart.
	siblingID := createProcessStrategy(ctx, t, db, submit)
	waitStrategyProcessQuotes(ctx, t, db, siblingID, 0)
	ids := []int64{prior.Id, siblingID}
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id IN ?", ids).UpdateColumn("desired_running", false).Error; err != nil {
		t.Fatal(err)
	}
	waitStrategyProcessesStopped(ctx, t, db, ids)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	core, _ := executionFixture(t, db)
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "BTC", "10", "0")
}

func createProcessStrategy(ctx context.Context, t *testing.T, db *gorm.DB, submit domain.GatewaySubmit) int64 {
	t.Helper()
	row := identityInstance(submit.Input.InstrumentID, "created-after-strategy-start", domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance)
	row.AccountId, row.DesiredRunning = submit.Input.AccountID, true
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Create(row).Error; err != nil {
			return err
		}
		parameters := storedParameterFixture(domain.StrategyTemplateMarketMaker)
		parameters.InstanceId = row.Id
		parameters.MakerParameters.InventoryHardLimit = "20"
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Create(parameters).Error; err != nil {
			return err
		}
		return tx.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Create(&modelv1.StrategyBudget{
			StrategyInstanceId: row.Id,
			AccountId:          row.AccountId,
			LimitNotional:      "300",
			ReservedNotional:   "0",
			Enabled:            true,
			Revision:           1,
		}).Error
	}); err != nil {
		t.Fatal(err)
	}
	return row.Id
}

func strategyProcessConfig(t *testing.T, ca *testcert.Authority, postgres *configv1.Postgres, naming *configv1.Nacos, account, instance string) *configv1.ServiceConfig {
	t.Helper()
	cfg := businessProcessConfig(t, ca, postgres, nil, naming, "strategy")
	cfg.InstanceId = "business-strategy." + instance
	cfg.Business.OperationTimeout = durationpb.New(time.Second)
	cfg.Business.PollInterval = durationpb.New(100 * time.Millisecond)
	cfg.Business.Strategy = &configv1.StrategyRuntime{
		AccountIds: []string{account},
		LeaseTtl:   durationpb.New(3 * time.Second),
	}
	cfg.GrpcClients = businessRPCConfig(ca.Issue(t, rpcsecurity.ServiceStrategy, false))
	cfg.GrpcClients.AllowedServices = []string{rpcsecurity.ServiceMarket, rpcsecurity.ServiceExecution}
	return cfg
}

func waitStrategyProcessQuotes(ctx context.Context, t *testing.T, db *gorm.DB, id, afterEpoch int64) *modelv1.StrategyInstance {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var row modelv1.StrategyInstance
		if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).First(&row, id).Error; err != nil {
			t.Fatal(err)
		}
		var orders []*modelv1.OrderIntent
		if err := db.WithContext(ctx).Model(&modelv1.OrderIntent{}).
			Where("strategy_instance_id = ? AND state = ? AND cancel_state = ? AND delete_at IS NULL", id, domain.OrderStateLive, domain.CancelStateNone).Find(&orders).Error; err != nil {
			t.Fatal(err)
		}
		buy, sell := false, false
		for _, order := range orders {
			buy = buy || order.Side == string(domain.SideBuy)
			sell = sell || order.Side == string(domain.SideSell)
		}
		if buy && sell && row.State == string(domain.StrategyStateRunning) && row.RuntimeOwnerEpoch > afterEpoch {
			return proto.CloneOf(&row)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("strategy %d did not acquire a newer owner and bilateral quotes: state=%s epoch=%d", id, row.State, row.RuntimeOwnerEpoch)
		case <-ticker.C:
		}
	}
}

func waitStrategyProcessesStopped(ctx context.Context, t *testing.T, db *gorm.DB, ids []int64) {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var count int64
		if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id IN ? AND state = ?", ids, domain.StrategyStateStopped).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count == int64(len(ids)) {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("strategy stop requests did not converge to terminal orders")
		case <-ticker.C:
		}
	}
}
