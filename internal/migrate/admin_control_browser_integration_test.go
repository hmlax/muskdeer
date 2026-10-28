//go:build integration && browser

package migrate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/types/known/durationpb"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestAdminControlBrowserRunsFundedGridAndKeepsOriginalRequestKey(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	_, execution, market, submit := gridControllerFixture(ctx, t, db)
	adminControlFixture(ctx, t, db)
	if err := db.WithContext(ctx).Model(&modelv1.AdminUser{}).Create(&modelv1.AdminUser{
		Username:          "control-viewer",
		PasswordHash:      controlFixturePasswordHash,
		Role:              string(domain.AdminRoleViewer),
		Enabled:           true,
		CredentialVersion: 1,
		Revision:          1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	prepareControlBrowserStoppedInstances(ctx, t, db, submit.Input.AccountID)
	expireLeaseFixture(ctx, t, db, submit.ExecutionAuthority.Key)
	expireLeaseFixture(ctx, t, db, market.publisher.Key)
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
	startBusinessProcessFor(ctx, t, marketProcessConfig(t, ca, postgres, naming, market.feed.ID, "browser-grid", "99.99", "100.01"), discovery, 90*time.Second, "")
	executionConfig := businessProcessConfig(t, ca, postgres, businessStream(ctx, t), naming, "execution")
	executionConfig.Business.OperationTimeout = durationpb.New(time.Second)
	executionConfig.Business.ExecutionLeaseTtl = durationpb.New(3 * time.Second)
	executionConfig.Business.Risk = &configv1.RiskRuntime{AccountIds: []string{submit.Input.AccountID}}
	executionConfig.Business.Simulator = &configv1.SimulatorScenarioRuntime{
		AccountIds:  []string{submit.Input.AccountID},
		CancelDelay: durationpb.New(100 * time.Millisecond),
	}
	startBusinessProcessFor(ctx, t, executionConfig, discovery, 90*time.Second, "")
	strategy := strategyProcessConfig(t, ca, postgres, naming, submit.Input.AccountID, "browser-grid")
	strategy.Business.Strategy.ManagementEnabled, strategy.Business.Strategy.ControlEnabled = true, true
	startBusinessProcessFor(ctx, t, strategy, discovery, 90*time.Second, "")
	waitControlBrowserState(ctx, t, db, submit.Input.Authority.StrategyID, domain.StrategyStateStopped)
	base := startStrategyManagementAPIFor(ctx, t, ca, postgres, naming, true, 90*time.Second)
	var latestID int64
	for _, phase := range []struct {
		name   string
		action domain.AdminControlAction
		state  domain.StrategyState
		quotes bool
	}{
		{
			name:   "start",
			action: domain.AdminControlStart,
			state:  domain.StrategyStateRunning,
			quotes: true,
		},
		{
			name:   "pause",
			action: domain.AdminControlPause,
			state:  domain.StrategyStatePaused,
		},
		{
			name:   "resume",
			action: domain.AdminControlResume,
			state:  domain.StrategyStateRunning,
			quotes: true,
		},
		{
			name:   "stop",
			action: domain.AdminControlStop,
			state:  domain.StrategyStateStopped,
		},
	} {
		runControlBrowserPhase(ctx, t, base, submit.Input.Authority.StrategyID, phase.name, latestID)
		waitControlBrowserState(ctx, t, db, submit.Input.Authority.StrategyID, phase.state)
		if phase.quotes {
			assertControlBrowserFundedQuotes(ctx, t, db, execution.core, submit)
		} else {
			assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
			assertCoreBalance(ctx, t, execution.core, submit.Input.AccountID, "USDT", "1000", "0")
			assertCoreBalance(ctx, t, execution.core, submit.Input.AccountID, "BTC", "10", "0")
		}
		var operation modelv1.AdminStrategyOperation
		if err := db.WithContext(ctx).Model(&modelv1.AdminStrategyOperation{}).Where("strategy_instance_id = ?", submit.Input.Authority.StrategyID).Order("id DESC").Take(&operation).Error; err != nil || operation.Action != string(phase.action) || operation.State != string(domain.AdminOperationSucceeded) || operation.GetObservedOwnerEpoch() <= 0 || operation.GetObservedState() != string(phase.state) {
			t.Fatal("browser did not persist the actual fenced runtime result", err)
		}
		latestID = operation.Id
	}
	runControlBrowserPhase(ctx, t, base, submit.Input.Authority.StrategyID, "viewer", latestID)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyOperation{}, 4)
	assertGatewayOrderCount(ctx, t, db, 8)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorFill{}, 0)
}

func prepareControlBrowserStoppedInstances(ctx context.Context, t *testing.T, db *gorm.DB, account string) {
	t.Helper()
	var instances []*modelv1.StrategyInstance
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("account_id = ?", account).Find(&instances).Error; err != nil {
		t.Fatal(err)
	}
	for _, instance := range instances {
		if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", instance.Id).
			Select("State", "DesiredState", "DesiredRunning", "Revision").Updates(&modelv1.StrategyInstance{
			State:          string(domain.StrategyStateStopped),
			DesiredState:   string(domain.StrategyStateStopped),
			DesiredRunning: false,
			Revision:       instance.Revision + 1,
		}).Error; err != nil {
			t.Fatal(err)
		}
		expireLeaseFixture(ctx, t, db, domain.LeaseKey{
			Kind:       domain.LeaseKindStrategy,
			StrategyID: instance.Id,
		})
	}
}

func waitControlBrowserState(ctx context.Context, t *testing.T, db *gorm.DB, id int64, state domain.StrategyState) {
	t.Helper()
	call, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	ticks := time.NewTicker(20 * time.Millisecond)
	defer ticks.Stop()
	for {
		var row modelv1.StrategyInstance
		if err := db.WithContext(call).Model(&modelv1.StrategyInstance{}).Where("id = ?", id).Take(&row).Error; err != nil {
			t.Fatal(err)
		}
		if row.State == string(state) && row.DesiredState == string(state) && row.RuntimeOwnerEpoch > 0 && row.GetActiveMarketDataSource() == string(domain.MarketSourceBinance) {
			return
		}
		select {
		case <-ticks.C:
		case <-call.Done():
			t.Fatal("actual browser strategy did not reach its fenced target")
		}
	}
}

func assertControlBrowserFundedQuotes(ctx context.Context, t *testing.T, db *gorm.DB, core domain.Simulator, submit domain.GatewaySubmit) {
	t.Helper()
	queries, err := repository.NewExecutionQueries(db)
	if err != nil {
		t.Fatal(err)
	}
	call, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	ticks := time.NewTicker(20 * time.Millisecond)
	defer ticks.Stop()
	for {
		orders, _, err := queries.Orders(call, domain.ExecutionFilter{
			AccountID:    submit.Input.AccountID,
			StrategyID:   submit.Input.Authority.StrategyID,
			InstrumentID: submit.Input.InstrumentID,
			Limit:        domain.MaximumExecutionPage,
			OpenOnly:     true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if gridControllerQuotesLive(orders) {
			assertGridPrices(t, orders)
			reserved, quoteFrozen, baseFrozen := decimal.Zero, decimal.Zero, decimal.Zero
			for _, order := range orders {
				actual, err := core.Find(call, order.Input.AccountID, order.ClientOrderID)
				if err != nil || actual.ID != order.CoreOrderID || actual.State != domain.OrderStateLive || !actual.FrozenAmount.IsPositive() {
					t.Fatal("browser quote bypassed actual funded core acceptance", err)
				}
				reserved = reserved.Add(order.Input.Price.Mul(order.Input.Quantity))
				switch actual.FrozenAsset {
				case "USDT":
					quoteFrozen = quoteFrozen.Add(actual.FrozenAmount)
				case "BTC":
					baseFrozen = baseFrozen.Add(actual.FrozenAmount)
				default:
					t.Fatal("browser quote froze an unexpected asset")
				}
			}
			assertCancellationResolutionBudgets(call, t, db, submit, reserved.String())
			assertCoreBalance(call, t, core, submit.Input.AccountID, "USDT", decimal.NewFromInt(1000).Sub(quoteFrozen).String(), quoteFrozen.String())
			assertCoreBalance(call, t, core, submit.Input.AccountID, "BTC", decimal.NewFromInt(10).Sub(baseFrozen).String(), baseFrozen.String())
			return
		}
		select {
		case <-ticks.C:
		case <-call.Done():
			t.Fatal("actual browser grid did not converge to four funded core quotes")
		}
	}
}

func runControlBrowserPhase(ctx context.Context, t *testing.T, base string, id int64, phase string, operationID int64) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, ".cache", "admin-control-browser")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	artifacts, err := os.MkdirTemp(parent, "run-"+phase+"-")
	if err != nil {
		t.Fatal(err)
	}
	username := "control-operator"
	if phase == "viewer" {
		username = "control-viewer"
	}
	command := exec.CommandContext(ctx, "python3", filepath.Join(root, "scripts", "web.py"), "browser-controls")
	command.Dir = root
	command.WaitDelay = 2 * time.Second
	command.Env = append(os.Environ(),
		"MUSKDEER_BROWSER_ORIGIN="+base,
		"MUSKDEER_BROWSER_USERNAME="+username,
		"MUSKDEER_BROWSER_PASSWORD="+adminFixturePassword,
		"MUSKDEER_BROWSER_ARTIFACTS="+artifacts,
		"MUSKDEER_BROWSER_STRATEGY_ID="+strconv.FormatInt(id, 10),
		"MUSKDEER_BROWSER_OPERATION_ID="+strconv.FormatInt(operationID, 10),
		"MUSKDEER_BROWSER_CONTROL_PHASE="+phase,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("actual control browser failed: %v\n%s", err, output)
	}
	t.Logf("actual funded control browser passed; phase=%s screenshots=%s", phase, artifacts)
}
