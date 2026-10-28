//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/ownerid"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/usecase"
)

func TestStrategyControllerQuotesProtectsAndWaitsForTerminalStop(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	portfolioFixture(ctx, t, db, submit)
	controller, execution, market := strategyControllerFixture(ctx, t, db, submit)
	for range 8 {
		strategyControllerTick(ctx, t, controller, market)
		time.Sleep(110 * time.Millisecond)
	}
	open, _, err := execution.Orders(ctx, domain.ExecutionFilter{
		AccountID:    submit.Input.AccountID,
		StrategyID:   submit.Input.Authority.StrategyID,
		InstrumentID: submit.Input.InstrumentID,
		Limit:        100,
		OpenOnly:     true,
	})
	if err != nil || len(open) < 2 {
		t.Fatalf("controller did not produce funded bilateral quotes: %d, %v", len(open), err)
	}
	buy, sell := false, false
	for _, order := range open {
		buy = buy || order.Input.Side == domain.SideBuy
		sell = sell || order.Input.Side == domain.SideSell
	}
	if !buy || !sell {
		t.Fatal("one side of the funded quote book is missing")
	}
	market.unavailable = true
	for range 6 {
		strategyControllerTick(ctx, t, controller, market)
	}
	var state modelv1.StrategyInstance
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).First(&state, submit.Input.Authority.StrategyID).Error; err != nil {
		t.Fatal(err)
	}
	if state.State != string(domain.StrategyStateRecovering) || execution.submits > 4 || execution.cancels == 0 {
		t.Fatalf("selected market loss did not protect existing quotes: %s submit=%d cancel=%d", state.State, execution.submits, execution.cancels)
	}
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", state.Id).UpdateColumn("desired_running", false).Error; err != nil {
		t.Fatal(err)
	}
	strategyControllerTick(ctx, t, controller, market)
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).First(&state, state.Id).Error; err != nil {
		t.Fatal(err)
	}
	if state.State != string(domain.StrategyStateStopping) {
		t.Fatal("accepting cancels falsely completed stop")
	}
	for _, order := range open {
		if _, err := execution.core.ConfirmCancel(ctx, order.Input.AccountID, order.ClientOrderID); err != nil {
			t.Fatal(err)
		}
	}
	for range 6 {
		strategyControllerTick(ctx, t, controller, market)
	}
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).First(&state, state.Id).Error; err != nil || state.State != string(domain.StrategyStateStopped) {
		t.Fatalf("terminal orders did not complete stop: %s, %v", state.State, err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertCoreBalance(ctx, t, execution.core, submit.Input.AccountID, "USDT", "1000", "0")
	assertCoreBalance(ctx, t, execution.core, submit.Input.AccountID, "BTC", "10", "0")
}

func TestStrategyControllerUnknownWriteIsOnlyObservedAcrossTakeover(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	portfolioFixture(ctx, t, db, submit)
	controller, execution, market := strategyControllerFixture(ctx, t, db, submit)
	execution.dropSubmit = true
	for range 6 {
		strategyControllerTick(ctx, t, controller, market)
	}
	if execution.submits != 1 {
		t.Fatalf("uncertain call was retried: %d", execution.submits)
	}
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	expireLeaseFixture(ctx, t, db, domain.LeaseKey{
		Kind:       domain.LeaseKindStrategy,
		StrategyID: submit.Input.Authority.StrategyID,
	})
	other := strategyControllerWith(ctx, t, db, submit, leases, execution, market)
	for range 6 {
		strategyControllerTick(ctx, t, other, market)
	}
	if execution.submits != 1 {
		t.Fatal("replacement owner replayed an absent uncertain submission")
	}
	actions, err := repository.NewStrategyActions(db)
	if err != nil {
		t.Fatal(err)
	}
	active, err := actions.Active(ctx, submit.Input.Authority.StrategyID)
	if err != nil || len(active) != 1 || active[0].State != domain.StrategyActionUnknown {
		t.Fatalf("uncertainty was erased: %+v, %v", active, err)
	}
	assertGatewayOrderCount(ctx, t, db, 0)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, active[0].Plan.Price.Mul(active[0].Plan.Quantity).String())
}

type controllerExecution struct {
	domain.ExecutionQueries
	core       domain.Simulator
	store      domain.IntentExecutionRepository
	intents    domain.IntentService
	requests   domain.CancellationService
	owner      domain.LeaseToken
	submits    int
	cancels    int
	dropSubmit bool
}

func (e *controllerExecution) Submit(ctx context.Context, input domain.OrderInput) (domain.IntentSnapshot, error) {
	e.submits++
	if e.dropSubmit {
		accepted, err := e.store.Accept(ctx, input, e.owner)
		if err != nil {
			return domain.IntentSnapshot{}, err
		}
		prepared, err := e.store.Prepare(ctx, accepted, input.Authority, e.owner, input.Deadline)
		if err != nil {
			return domain.IntentSnapshot{}, err
		}
		if _, err := e.store.Unknown(ctx, prepared, domain.IntentErrorOutcomeUnknown); err != nil {
			return domain.IntentSnapshot{}, err
		}
		return domain.IntentSnapshot{}, domain.ErrOutcomeUnknown
	}
	return e.intents.Submit(ctx, input, e.owner)
}

func (e *controllerExecution) Cancel(ctx context.Context, input domain.CancelInput) (domain.CancellationResult, error) {
	e.cancels++
	return e.requests.Cancel(ctx, input, e.owner)
}

func (e *controllerExecution) FindOrder(ctx context.Context, account, key string) (domain.IntentSnapshot, error) {
	clientID, err := domain.IntentClientOrderID(account, key)
	if err != nil {
		return domain.IntentSnapshot{}, err
	}
	return e.Order(ctx, account, clientID)
}

func (e *controllerExecution) Order(ctx context.Context, account, clientID string) (domain.IntentSnapshot, error) {
	row, err := e.store.ByClient(ctx, account, clientID)
	if err != nil {
		return domain.IntentSnapshot{}, err
	}
	return e.intents.Recover(ctx, account, row.ID)
}

func (e *controllerExecution) FindCancellation(ctx context.Context, account, key string) (domain.CancellationResult, error) {
	return e.requests.RecoverByKey(ctx, account, key)
}

type controllerMarket struct {
	feed        domain.MarketFeedDefinition
	publisher   domain.LeaseToken
	store       domain.MarketRepository
	evaluator   domain.RiskEvaluator
	strategyID  int64
	unavailable bool
}

func (m *controllerMarket) Reference(ctx context.Context, source domain.MarketSource, instrumentID int64, mode domain.MarketMode) (domain.MarketReference, error) {
	if m.unavailable {
		return domain.MarketReference{}, domain.ErrUnreliableMarket
	}
	if source != m.feed.Source || instrumentID != m.feed.InstrumentID || mode != m.feed.Mode {
		return domain.MarketReference{}, domain.ErrMarketIdentity
	}
	value, err := m.store.Get(ctx, m.feed.ID)
	return value.MarketReference, err
}

func strategyControllerTick(ctx context.Context, t *testing.T, controller *usecase.StrategyController, market *controllerMarket) {
	t.Helper()
	strategyControllerTickAt(ctx, t, controller, market, "100")
}

func strategyControllerTickAt(ctx context.Context, t *testing.T, controller *usecase.StrategyController, market *controllerMarket, midpoint string) {
	t.Helper()
	publication, err := market.store.Get(ctx, market.feed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := market.store.Publish(ctx, market.publisher, publication.Revision, marketReadingFixture(market.feed, publication.Reading.Sequence+1, midpoint)); err != nil {
		t.Fatal(err)
	}
	work, cancel := context.WithTimeout(ctx, 2*time.Second)
	_, err = controller.Tick(work)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := market.evaluator.Evaluate(ctx, market.strategyID); err != nil {
		t.Fatal(err)
	}
}

func strategyControllerFixture(ctx context.Context, t *testing.T, db *gorm.DB, submit domain.GatewaySubmit) (*usecase.StrategyController, *controllerExecution, *controllerMarket) {
	t.Helper()
	core, store := executionFixture(t, db)
	queries, err := repository.NewExecutionQueries(db)
	if err != nil {
		t.Fatal(err)
	}
	execution := &controllerExecution{
		ExecutionQueries: queries,
		core:             core,
		store:            store,
		intents:          intentServiceFixture(t, store, core),
		requests:         cancellationServiceFixture(t, db, store, core),
		owner:            submit.ExecutionAuthority,
	}
	feed, publisher, snapshots, _ := admissionSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	evaluator, err := repository.NewRiskEvaluator(db)
	if err != nil {
		t.Fatal(err)
	}
	market := &controllerMarket{
		feed:       feed,
		publisher:  publisher,
		store:      snapshots,
		evaluator:  evaluator,
		strategyID: submit.Input.Authority.StrategyID,
	}
	expireLeaseFixture(ctx, t, db, domain.LeaseKey{
		Kind:       domain.LeaseKindStrategy,
		StrategyID: submit.Input.Authority.StrategyID,
	})
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	return strategyControllerWith(ctx, t, db, submit, leases, execution, market), execution, market
}

func strategyControllerWith(ctx context.Context, t *testing.T, db *gorm.DB, submit domain.GatewaySubmit, leases domain.LeaseRepository, execution domain.StrategyExecution, market domain.MarketReader, wraps ...func(domain.StrategyLifecycle) domain.StrategyLifecycle) *usecase.StrategyController {
	t.Helper()
	lifecycle, err := repository.NewStrategyLifecycle(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, wrap := range wraps {
		lifecycle = wrap(lifecycle)
	}
	environment, err := repository.NewStrategyEnvironment(db)
	if err != nil {
		t.Fatal(err)
	}
	actions, err := repository.NewStrategyActions(db)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := ownerid.New(ctx, "controller-test")
	if err != nil {
		t.Fatal(err)
	}
	controller, err := usecase.NewStrategyController(usecase.StrategyDependencies{
		Leases:      leases,
		Lifecycle:   lifecycle,
		Environment: environment,
		Actions:     actions,
		Execution:   execution,
		Market:      market,
	}, usecase.StrategySettings{
		StrategyID: submit.Input.Authority.StrategyID,
		OwnerID:    owner,
		LeaseTTL:   5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return controller
}

var _ domain.StrategyExecution = (*controllerExecution)(nil)

func TestStrategyControllerReobservesConcurrentControlRequest(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	portfolioFixture(ctx, t, db, submit)
	_, execution, market := strategyControllerFixture(ctx, t, db, submit)
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	controller := strategyControllerWith(ctx, t, db, submit, leases, execution, market, func(inner domain.StrategyLifecycle) domain.StrategyLifecycle {
		return &concurrentStopLifecycle{
			StrategyLifecycle: inner,
			db:                db,
		}
	})
	strategyControllerTick(ctx, t, controller, market)
	for range 3 {
		strategyControllerTick(ctx, t, controller, market)
	}
	var row modelv1.StrategyInstance
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).First(&row, submit.Input.Authority.StrategyID).Error; err != nil {
		t.Fatal(err)
	}
	if row.State != string(domain.StrategyStateStopped) || row.DesiredRunning || execution.submits != 0 {
		t.Fatal("concurrent stop was lost or generated a trading request")
	}
	assertGatewayOrderCount(ctx, t, db, 0)
}

type concurrentStopLifecycle struct {
	domain.StrategyLifecycle
	db      *gorm.DB
	changed bool
}

func (s *concurrentStopLifecycle) Instance(ctx context.Context, id int64) (domain.StrategySpec, error) {
	row, err := s.StrategyLifecycle.Instance(ctx, id)
	if err != nil || s.changed {
		return row, err
	}
	s.changed = true
	// Commit the management request after the controller read, before its CAS.
	err = s.db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", id).
		Select("DesiredRunning", "Revision").Updates(&modelv1.StrategyInstance{
		DesiredRunning: false,
		Revision:       row.Revision + 1,
	}).Error
	return row, err
}
