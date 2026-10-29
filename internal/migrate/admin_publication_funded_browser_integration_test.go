//go:build integration && browser

package migrate

import (
	"context"
	"database/sql"
	"errors"
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

func TestAdminPublicationBrowserRunsFundedTemplatesAndWaitsForSelectedSource(t *testing.T) {
	for _, template := range []domain.StrategyTemplate{domain.StrategyTemplateMarketMaker, domain.StrategyTemplateGrid} {
		t.Run(string(template), func(t *testing.T) { fundedPublicationScenario(t, template) })
	}
}

func fundedPublicationScenario(t *testing.T, template domain.StrategyTemplate) {
	t.Helper()
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	prepareControlBrowserStoppedInstances(ctx, t, db, submit.Input.AccountID)
	request := adminCreationRequest(t, submit, template, domain.MarketSourceBinance)
	request.InstanceKey = "funded-publication-" + string(template)
	if request.Parameters.Maker != nil {
		request.Parameters.Maker.InventoryTarget = decimal.NewFromInt(10)
		request.Parameters.Maker.InventorySoftLimit = decimal.NewFromInt(15)
		request.Parameters.Maker.InventoryHardLimit = decimal.NewFromInt(20)
		request.Parameters.Maker.MaxQuoteAge = 30 * time.Second
		request.Parameters.Maker.MaxMarketAge = 2 * time.Second
	} else {
		request.Parameters.Grid.LowerPrice, request.Parameters.Grid.UpperPrice = decimal.NewFromInt(98), decimal.NewFromInt(102)
		request.Parameters.Grid.GridCount = 5
		request.Parameters.Grid.TotalBudget, request.Parameters.Grid.PerOrderNotional = decimal.NewFromInt(50), decimal.NewFromInt(10)
		request.Parameters.Grid.InventoryLimit = decimal.NewFromInt(20)
		request.Parameters.Grid.MaxMarketAge = 2 * time.Second
	}
	created, err := creator.Create(ctx, actor.Token, actor.CSRF, request)
	if err != nil {
		t.Fatal(err)
	}
	submit.Input.Authority.StrategyID = created.StrategyID
	oldBudget := activationBudget(ctx, t, db, created.StrategyID)
	oldLimit := decimal.RequireFromString(oldBudget.LimitNotional)
	feeds, err := repository.NewMarketFeeds(db)
	if err != nil {
		t.Fatal(err)
	}
	binance, owner, _, _ := admissionSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	okx, err := feeds.Find(ctx, domain.MarketSourceOKX, submit.Input.InstrumentID, domain.MarketModeReplay)
	if err != nil {
		t.Fatal(err)
	}
	expireLeaseFixture(ctx, t, db, owner.Key)
	expireLeaseFixture(ctx, t, db, submit.ExecutionAuthority.Key)
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
	startBusinessProcessFor(ctx, t, marketProcessConfig(t, ca, postgres, naming, binance.ID, "funded-"+string(template)+"-binance", "99.99", "100.01"), discovery, 90*time.Second, "")
	execution := businessProcessConfig(t, ca, postgres, businessStream(ctx, t), naming, "execution")
	execution.Business.OperationTimeout = durationpb.New(time.Second)
	execution.Business.ExecutionLeaseTtl = durationpb.New(3 * time.Second)
	execution.Business.Risk = &configv1.RiskRuntime{AccountIds: []string{submit.Input.AccountID}}
	execution.Business.Simulator = &configv1.SimulatorScenarioRuntime{
		AccountIds:  []string{submit.Input.AccountID},
		CancelDelay: durationpb.New(500 * time.Millisecond),
	}
	startBusinessProcessFor(ctx, t, execution, discovery, 90*time.Second, "")
	strategy := strategyProcessConfig(t, ca, postgres, naming, submit.Input.AccountID, "funded-publication-"+string(template))
	strategy.Business.Strategy.ManagementEnabled, strategy.Business.Strategy.ControlEnabled = true, true
	startBusinessProcessFor(ctx, t, strategy, discovery, 90*time.Second, "")
	base := startStrategyManagementAPIFor(ctx, t, ca, postgres, naming, true, 90*time.Second)
	core, _ := executionFixture(t, db)
	waitFundedPublicationState(ctx, t, db, created.StrategyID, domain.StrategyStateStopped)
	runControlBrowserPhase(ctx, t, base, created.StrategyID, "start", 0)
	assertFundedPublicationQuotes(ctx, t, db, core, submit, 1, binance, oldLimit)
	runFundedPublicationPhase(ctx, t, db, base, created.StrategyID, template, "running", 2, domain.MarketSourceOKX, "6", "15", oldLimit, true)
	waitColdPublicationDrain(ctx, t, db, core, submit, oldLimit)
	startBusinessProcessFor(ctx, t, marketProcessConfig(t, ca, postgres, naming, okx.ID, "funded-"+string(template)+"-okx", "99.99", "100.01"), discovery, 90*time.Second, "")
	assertFundedPublicationQuotes(ctx, t, db, core, submit, 2, okx, decimal.NewFromInt(30))
	runControlBrowserPhase(ctx, t, base, created.StrategyID, "pause", 0)
	waitFundedPublicationState(ctx, t, db, created.StrategyID, domain.StrategyStatePaused)
	maximum := strategyMaximumIntentID(ctx, t, db, created.StrategyID)
	runFundedPublicationPhase(ctx, t, db, base, created.StrategyID, template, "paused", 3, domain.MarketSourceBinance, "8", "20", decimal.NewFromInt(30), false)
	assertPublicationKeepsTarget(ctx, t, db, submit, domain.StrategyStatePaused, 3, 2, decimal.NewFromInt(30), maximum)
	runControlBrowserPhase(ctx, t, base, created.StrategyID, "resume", 0)
	assertFundedPublicationQuotes(ctx, t, db, core, submit, 3, binance, decimal.NewFromInt(40))
	runControlBrowserPhase(ctx, t, base, created.StrategyID, "stop", 0)
	waitFundedPublicationState(ctx, t, db, created.StrategyID, domain.StrategyStateStopped)
	maximum = strategyMaximumIntentID(ctx, t, db, created.StrategyID)
	runFundedPublicationPhase(ctx, t, db, base, created.StrategyID, template, "stopped", 4, domain.MarketSourceOKX, "7", "17.5", decimal.NewFromInt(40), false)
	assertPublicationKeepsTarget(ctx, t, db, submit, domain.StrategyStateStopped, 4, 3, decimal.NewFromInt(40), maximum)
	assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "BTC", "10", "0")
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyPublication{}, 3)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyOperation{}, 4)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorFill{}, 0)
}

func waitFundedPublicationState(ctx context.Context, t *testing.T, db *gorm.DB, id int64, state domain.StrategyState) {
	t.Helper()
	call, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		instance := publicationInstance(call, t, db, id)
		if instance.State == string(state) && instance.DesiredState == string(state) && instance.RuntimeOwnerEpoch > 0 {
			return
		}
		select {
		case <-ticker.C:
		case <-call.Done():
			t.Fatal("funded publication did not reach its actual target")
		}
	}
}

func assertFundedPublicationQuotes(ctx context.Context, t *testing.T, db *gorm.DB, core domain.Simulator, submit domain.GatewaySubmit, version int64, feed domain.MarketFeedDefinition, limit decimal.Decimal) {
	t.Helper()
	queries, err := repository.NewExecutionQueries(db)
	if err != nil {
		t.Fatal(err)
	}
	call, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		orders, _, err := queries.Orders(call, domain.ExecutionFilter{
			AccountID:  submit.Input.AccountID,
			StrategyID: submit.Input.Authority.StrategyID,
			OpenOnly:   true,
			Limit:      domain.MaximumExecutionPage,
		})
		if err != nil {
			t.Fatal(err)
		}
		instance := publicationInstance(call, t, db, submit.Input.Authority.StrategyID)
		ready := len(orders) == 4 && instance.State == string(domain.StrategyStateRunning) && instance.GetActiveConfigVersion() == version && instance.GetActiveMarketDataSource() == string(feed.Source)
		for _, order := range orders {
			ready = ready && order.State == domain.OrderStateLive && order.Input.Authority.ConfigVersion == version
		}
		if ready {
			var risk modelv1.RiskSnapshot
			if err := db.WithContext(call).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ?", instance.Id).First(&risk).Error; err != nil || !risk.AllowNewRisk || risk.GetMarketFeedId() != feed.ID || risk.ConfigVersion != version || risk.MarketDataSource != string(feed.Source) {
				t.Fatal("funded quotes lack the exact selected-source risk proof", err)
			}
			budget := activationBudget(call, t, db, instance.Id)
			if !decimal.RequireFromString(budget.LimitNotional).Equal(limit) {
				t.Fatal("active budget did not follow the published template")
			}
			reserved, quoteFrozen, baseFrozen := decimal.Zero, decimal.Zero, decimal.Zero
			for _, order := range orders {
				actual, err := core.Find(call, submit.Input.AccountID, order.ClientOrderID)
				if err != nil || actual.State != domain.OrderStateLive || actual.ID != order.CoreOrderID || !actual.FrozenAmount.IsPositive() {
					t.Fatal("published quote bypassed funded core acceptance", err)
				}
				reserved = reserved.Add(order.Input.Price.Mul(order.Input.Quantity))
				switch actual.FrozenAsset {
				case "USDT":
					quoteFrozen = quoteFrozen.Add(actual.FrozenAmount)
				case "BTC":
					baseFrozen = baseFrozen.Add(actual.FrozenAmount)
				default:
					t.Fatal("published quote froze an unexpected asset")
				}
			}
			if reserved.GreaterThan(limit) {
				t.Fatal("published quotes exceeded their active limit")
			}
			assertCancellationResolutionBudgets(call, t, db, submit, reserved.String())
			assertCoreBalance(call, t, core, submit.Input.AccountID, "USDT", decimal.NewFromInt(1000).Sub(quoteFrozen).String(), quoteFrozen.String())
			assertCoreBalance(call, t, core, submit.Input.AccountID, "BTC", decimal.NewFromInt(10).Sub(baseFrozen).String(), baseFrozen.String())
			return
		}
		select {
		case <-ticker.C:
		case <-call.Done():
			t.Fatal("published version did not converge to four funded quotes")
		}
	}
}

func waitColdPublicationDrain(ctx context.Context, t *testing.T, db *gorm.DB, core domain.Simulator, submit domain.GatewaySubmit, limit decimal.Decimal) {
	t.Helper()
	call, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		instance := publicationInstance(call, t, db, submit.Input.Authority.StrategyID)
		budget := activationBudget(call, t, db, instance.Id)
		if instance.GetActiveConfigVersion() != 1 || instance.GetActiveMarketDataSource() != string(domain.MarketSourceBinance) || !instance.DesiredRunning || instance.RequestedConfigVersion != 2 || instance.MarketDataSource != string(domain.MarketSourceOKX) || !decimal.RequireFromString(budget.LimitNotional).Equal(limit) {
			t.Fatal("unhealthy new source activated, changed target or replaced old capacity")
		}
		var count int64
		if err := db.WithContext(call).Model(&modelv1.OrderIntent{}).Where("strategy_instance_id = ? AND state NOT IN ?", instance.Id, []domain.OrderState{domain.OrderStateFilled, domain.OrderStateCanceled, domain.OrderStateRejected}).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count == 0 && decimal.RequireFromString(budget.ReservedNotional).IsZero() {
			assertCancellationResolutionBudgets(call, t, db, submit, "0")
			assertCoreBalance(call, t, core, submit.Input.AccountID, "USDT", "1000", "0")
			assertCoreBalance(call, t, core, submit.Input.AccountID, "BTC", "10", "0")
			return
		}
		select {
		case <-ticker.C:
		case <-call.Done():
			t.Fatal("old funded obligations did not converge before new-source startup")
		}
	}
}

func assertPublicationKeepsTarget(ctx context.Context, t *testing.T, db *gorm.DB, submit domain.GatewaySubmit, state domain.StrategyState, requested, active int64, limit decimal.Decimal, maximum int64) {
	t.Helper()
	var lease modelv1.StrategyLease
	if err := db.WithContext(ctx).Model(&modelv1.StrategyLease{}).Where("strategy_instance_id = ?", submit.Input.Authority.StrategyID).First(&lease).Error; err != nil {
		t.Fatal(err)
	}
	call, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		instance := publicationInstance(call, t, db, submit.Input.Authority.StrategyID)
		budget := activationBudget(call, t, db, instance.Id)
		if instance.State != string(state) || instance.DesiredState != string(state) || instance.DesiredRunning || instance.RequestedConfigVersion != requested || instance.GetActiveConfigVersion() != active || !decimal.RequireFromString(budget.LimitNotional).Equal(limit) || !decimal.RequireFromString(budget.ReservedNotional).IsZero() || strategyMaximumIntentID(call, t, db, instance.Id) != maximum {
			t.Fatal("publication implicitly resumed a quiescent target or activated its new limit")
		}
		var current modelv1.StrategyLease
		if err := db.WithContext(call).Model(&modelv1.StrategyLease{}).Where("id = ?", lease.Id).First(&current).Error; err != nil {
			t.Fatal(err)
		}
		if current.ExpiresAt >= lease.ExpiresAt+200 {
			return
		}
		select {
		case <-ticker.C:
		case <-call.Done():
			t.Fatal("quiescent owner did not demonstrate progress after publication")
		}
	}
}

type publicationBarrierResult struct {
	held bool
	err  error
}

func observePublicationBarrier(ctx context.Context, db *gorm.DB, id int64, oldLimit decimal.Decimal, stop <-chan struct{}) publicationBarrierResult {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	result := publicationBarrierResult{}
	for {
		select {
		case <-stop:
			return result
		case <-ctx.Done():
			result.err = ctx.Err()
			return result
		case <-ticker.C:
		}
		call, cancel := context.WithTimeout(ctx, time.Second)
		var instance modelv1.StrategyInstance
		var budget modelv1.StrategyBudget
		err := db.WithContext(call).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
			if err := tx.WithContext(call).Model(&modelv1.StrategyInstance{}).Where("id = ?", id).First(&instance).Error; err != nil {
				return err
			}
			return tx.WithContext(call).Model(&modelv1.StrategyBudget{}).Where("strategy_instance_id = ?", id).First(&budget).Error
		}, &sql.TxOptions{
			Isolation: sql.LevelRepeatableRead,
			ReadOnly:  true,
		})
		cancel()
		if err != nil {
			result.err = err
			return result
		}
		if instance.RequestedConfigVersion < 2 {
			continue
		}
		limit, limitErr := domain.ParseLedgerDecimal(budget.LimitNotional, false, false)
		reserved, reserveErr := domain.ParseLedgerDecimal(budget.ReservedNotional, false, true)
		if err := errors.Join(limitErr, reserveErr); err != nil {
			result.err = err
			return result
		}
		if instance.GetActiveConfigVersion() != 1 || !limit.Equal(oldLimit) {
			result.err = errors.New("cold source replaced active input or its limit")
			return result
		}
		result.held = result.held || reserved.GreaterThan(decimal.NewFromInt(30))
	}
}

func runFundedPublicationPhase(ctx context.Context, t *testing.T, db *gorm.DB, base string, id int64, template domain.StrategyTemplate, phase string, version int64, source domain.MarketSource, notional, makerBudget string, oldLimit decimal.Decimal, observe bool) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, ".cache", "admin-funded-publication-browser")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	artifacts, err := os.MkdirTemp(parent, "run-"+string(template)+"-"+phase+"-")
	if err != nil {
		t.Fatal(err)
	}
	budget := decimal.RequireFromString(makerBudget)
	if template == domain.StrategyTemplateGrid {
		budget = budget.Mul(decimal.NewFromInt(2))
	}
	command := exec.CommandContext(ctx, "python3", filepath.Join(root, "scripts", "web.py"), "browser-funded-publication")
	command.Dir, command.WaitDelay = root, 2*time.Second
	command.Env = append(os.Environ(), "MUSKDEER_BROWSER_ORIGIN="+base, "MUSKDEER_BROWSER_PASSWORD="+adminFixturePassword, "MUSKDEER_BROWSER_ARTIFACTS="+artifacts,
		"MUSKDEER_BROWSER_STRATEGY_ID="+strconv.FormatInt(id, 10), "MUSKDEER_BROWSER_TEMPLATE="+string(template), "MUSKDEER_BROWSER_PUBLICATION_PHASE="+phase,
		"MUSKDEER_BROWSER_SOURCE="+string(source), "MUSKDEER_BROWSER_VERSION="+strconv.FormatInt(version, 10), "MUSKDEER_BROWSER_NOTIONAL="+notional, "MUSKDEER_BROWSER_PARAMETER_BUDGET="+budget.String())
	if !observe {
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("funded publication browser failed: %v\n%s", err, output)
		}
	} else {
		stop := make(chan struct{})
		result := make(chan publicationBarrierResult, 1)
		go func() { result <- observePublicationBarrier(ctx, db, id, oldLimit, stop) }()
		output, commandErr := command.CombinedOutput()
		close(stop)
		observed := <-result
		if commandErr != nil || observed.err != nil || !observed.held {
			t.Fatalf("publication did not preserve held old capacity: command=%v observation=%v held=%t\n%s", commandErr, observed.err, observed.held, output)
		}
	}
	t.Logf("funded publication %s %s v%d passed; screenshots=%s", template, phase, version, artifacts)
}
