//go:build integration

package migrate

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/simulator"
	"github.com/hmlax/muskdeer/internal/usecase"
)

type observedGateway struct {
	domain.TradingGateway
	submits atomic.Int64
	queries atomic.Int64
	before  func(context.Context, domain.GatewaySubmit) error
	failure error
}

func (g *observedGateway) Submit(ctx context.Context, command domain.GatewaySubmit) (domain.CoreOrder, error) {
	g.submits.Add(1)
	if g.before != nil {
		if err := g.before(ctx, command); err != nil {
			return domain.CoreOrder{}, err
		}
	}
	if g.failure != nil {
		return domain.CoreOrder{}, g.failure
	}
	return g.TradingGateway.Submit(ctx, command)
}

func (g *observedGateway) Find(ctx context.Context, accountID, clientID string) (domain.CoreOrder, error) {
	g.queries.Add(1)
	return g.TradingGateway.Find(ctx, accountID, clientID)
}

func TestOMSUnknownQueriesExistingCoreOrderAndKeepsReservedRisk(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, store := executionFixture(t, db)
	lost, err := simulator.WithFaults(core, simulator.FaultPlan{
		Submit: simulator.ResponseLost,
		Cancel: simulator.ResponseNormal,
	})
	if err != nil {
		t.Fatal(err)
	}
	observed := &observedGateway{TradingGateway: lost}
	service := intentServiceFixture(t, store, observed)
	unknown, err := service.Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil || unknown.State != domain.OrderStateUnknown || observed.submits.Load() != 1 {
		t.Fatalf("OMS unknown submission = %+v, %v", unknown, err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
	assertGatewayOrderCount(ctx, t, db, 1)
	confirmed, err := service.Recover(ctx, submit.Input.AccountID, unknown.ID)
	if err != nil || confirmed.State != domain.OrderStateLive || confirmed.CoreOrderID <= 0 || observed.submits.Load() != 1 || observed.queries.Load() != 1 {
		t.Fatalf("OMS did not query-first recover: %+v, %v", confirmed, err)
	}
	if repeated, err := service.Submit(ctx, submit.Input, submit.ExecutionAuthority); err != nil || repeated.State != domain.OrderStateLive || observed.submits.Load() != 1 {
		t.Fatalf("repeated intent resubmitted core order: %+v, %v", repeated, err)
	}
	oldCore, err := core.Find(ctx, submit.Input.AccountID, confirmed.ClientOrderID)
	if err != nil {
		t.Fatal(err)
	}
	contradictory := oldCore
	contradictory.State = domain.OrderStateCanceled
	contradictory.CancelState = domain.CancelStateConfirmed
	if _, err := store.Confirm(ctx, confirmed, contradictory); !errors.Is(err, domain.ErrInvalidOrder) {
		t.Fatalf("same core revision changed its business facts: %v", err)
	}
	fill := domain.CoreFillRequest{
		AccountID:           submit.Input.AccountID,
		ClientOrderID:       confirmed.ClientOrderID,
		TradeID:             "oms-fill-1",
		CounterpartySubject: "external-liquidity",
		Price:               decimal.NewFromInt(100),
		Quantity:            decimal.NewFromInt(1),
	}
	if _, err := core.Fill(ctx, fill); err != nil {
		t.Fatal(err)
	}
	partial, err := service.Recover(ctx, submit.Input.AccountID, unknown.ID)
	if err != nil || partial.State != domain.OrderStatePartiallyFilled || !partial.FilledQuantity.Equal(decimal.NewFromInt(1)) {
		t.Fatalf("OMS partial state = %+v, %v", partial, err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "100")
	if retained, err := store.Confirm(ctx, partial, oldCore); err != nil || retained.State != domain.OrderStatePartiallyFilled || !retained.ReservedQuoteNotional.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("older core snapshot regressed state or reservation: %+v, %v", retained, err)
	}
	blocked := submit.Input
	blocked.IdempotencyKey = "unreconciled-risk"
	if _, err := store.Accept(ctx, blocked, submit.ExecutionAuthority); !errors.Is(err, domain.ErrRiskDenied) {
		t.Fatalf("unprojected execution admitted new risk: %v", err)
	}
	fill.TradeID = "oms-fill-2"
	if _, err := core.Fill(ctx, fill); err != nil {
		t.Fatal(err)
	}
	filled, err := service.Recover(ctx, submit.Input.AccountID, unknown.ID)
	if err != nil || filled.State != domain.OrderStateFilled || !filled.ReservedQuoteNotional.IsZero() {
		t.Fatalf("OMS terminal state = %+v, %v", filled, err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	if _, err := store.Confirm(ctx, filled, domain.CoreOrder{}); !errors.Is(err, domain.ErrInvalidOrder) {
		t.Fatalf("invalid core snapshot replaced final state: %v", err)
	}
}

func TestOMSConfirmedCoreRejectionReleasesOnlyItsReservation(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, store := executionFixture(t, db)
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorBalance{}).Where("account_id = ? AND asset = ?", submit.Input.AccountID, "USDT").UpdateColumn("available", "0").Error; err != nil {
		t.Fatal(err)
	}
	service := intentServiceFixture(t, store, core)
	rejected, err := service.Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil || rejected.State != domain.OrderStateRejected || !rejected.ReservedQuoteNotional.IsZero() {
		t.Fatalf("confirmed rejection = %+v, %v", rejected, err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertGatewayOrderCount(ctx, t, db, 0)
}

func TestOMSFinalTargetRejectsRiskChangedAfterPrepare(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, store := executionFixture(t, db)
	observed := &observedGateway{
		TradingGateway: core,
		before: func(ctx context.Context, command domain.GatewaySubmit) error {
			prepared, err := store.ByID(ctx, command.Input.AccountID, command.IntentID)
			if err != nil || prepared.State != domain.OrderStateSubmitting {
				t.Fatalf("gateway called before durable SUBMITTING: %+v, %v", prepared, err)
			}
			return db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ?", command.Input.Authority.StrategyID).UpdateColumn("allow_new_risk", false).Error
		},
	}
	service := intentServiceFixture(t, store, observed)
	rejected, err := service.Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil || rejected.State != domain.OrderStateRejected || rejected.ErrorCode != domain.IntentErrorRiskDenied {
		t.Fatalf("changed final risk accepted an order: %+v, %v", rejected, err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertGatewayOrderCount(ctx, t, db, 0)
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
}

func TestOMSAbsenceAfterUnknownDoesNotBlindlyResubmit(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, store := executionFixture(t, db)
	observed := &observedGateway{
		TradingGateway: core,
		failure:        domain.ErrOutcomeUnknown,
	}
	service := intentServiceFixture(t, store, observed)
	unknown, err := service.Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil || unknown.State != domain.OrderStateUnknown {
		t.Fatalf("ambiguous absence = %+v, %v", unknown, err)
	}
	stillUnknown, err := service.Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil || stillUnknown.State != domain.OrderStateUnknown || stillUnknown.ErrorCode != domain.IntentErrorCoreNotFound || observed.submits.Load() != 1 || observed.queries.Load() != 1 {
		t.Fatalf("uncertain absence was replayed: %+v, %v", stillUnknown, err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
	assertGatewayOrderCount(ctx, t, db, 0)
}

func TestOMSCallerLossAfterCommitRecoversDurableSubmitting(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, store := executionFixture(t, db)
	committed, gate := make(chan struct{}, 1), make(chan struct{})
	delayed, err := simulator.WithFaults(core, simulator.FaultPlan{
		Submit:    simulator.ResponseWait,
		Cancel:    simulator.ResponseNormal,
		Committed: committed,
		Gate:      gate,
	})
	if err != nil {
		t.Fatal(err)
	}
	service := intentServiceFixture(t, store, delayed)
	callCtx, stop := context.WithCancelCause(ctx)
	var workers sync.WaitGroup
	result := make(chan error, 1)
	t.Cleanup(func() {
		stop(nil)
		workers.Wait()
	})
	workers.Go(func() {
		_, err := service.Submit(callCtx, submit.Input, submit.ExecutionAuthority)
		result <- err
	})
	select {
	case <-committed:
	case <-ctx.Done():
		t.Fatal("OMS did not reach committed core")
	}
	stop(context.DeadlineExceeded)
	select {
	case err := <-result:
		if !errors.Is(err, domain.ErrOutcomeUnknown) {
			t.Fatalf("caller loss pretended a confirmed rejection: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("OMS did not join canceled caller")
	}
	var persisted modelv1.OrderIntent
	if err := db.WithContext(ctx).Model(&modelv1.OrderIntent{}).First(&persisted).Error; err != nil || persisted.State != string(domain.OrderStateSubmitting) {
		t.Fatalf("lost caller did not retain recoverable SUBMITTING: %v, %v", &persisted, err)
	}
	recovered, err := service.Recover(ctx, submit.Input.AccountID, persisted.Id)
	if err != nil || recovered.State != domain.OrderStateLive {
		t.Fatalf("durable SUBMITTING recovery = %+v, %v", recovered, err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
	assertGatewayOrderCount(ctx, t, db, 1)
}

func TestOMSRecoveryDuringUncommittedCoreCommandDoesNotDeadlock(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, store := executionFixture(t, db)
	service := intentServiceFixture(t, store, core)
	staged, release := make(chan struct{}, 1), make(chan struct{})
	var releaseOnce sync.Once
	const callbackName = "muskdeer:test:hold-core-before-commit"
	if err := db.Callback().Create().After("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*modelv1.SimulatorOrder); !ok {
			return
		}
		staged <- struct{}{}
		select {
		case <-release:
		case <-tx.Statement.Context.Done():
			tx.Error = tx.Statement.Context.Err()
		}
	}); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	result := make(chan error, 1)
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		cancel()
		workers.Wait()
		if err := db.Callback().Create().Remove(callbackName); err != nil {
			t.Error(err)
		}
	})
	workers.Go(func() {
		_, err := service.Submit(ctx, submit.Input, submit.ExecutionAuthority)
		result <- err
	})
	select {
	case <-staged:
	case <-ctx.Done():
		t.Fatal("core order did not reach the held transaction boundary")
	}
	var persisted modelv1.OrderIntent
	if err := db.WithContext(ctx).Model(&modelv1.OrderIntent{}).First(&persisted).Error; err != nil {
		t.Fatal(err)
	}
	probeCtx, stopProbe := context.WithTimeout(ctx, 2*time.Second)
	defer stopProbe()
	unknown, err := service.Recover(probeCtx, submit.Input.AccountID, persisted.Id)
	if err != nil || unknown.State != domain.OrderStateUnknown {
		t.Fatalf("recovery blocked on the in-flight intent FK: %+v, %v", unknown, err)
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("core command did not finish after recovery released its budget locks")
	}
	confirmed, err := service.Recover(ctx, submit.Input.AccountID, persisted.Id)
	if err != nil || confirmed.State != domain.OrderStateLive {
		t.Fatalf("in-flight recovered order = %+v, %v", confirmed, err)
	}
	assertGatewayOrderCount(ctx, t, db, 1)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
}

func executionFixture(t *testing.T, db *gorm.DB) (domain.Simulator, domain.IntentExecutionRepository) {
	t.Helper()
	core, err := repository.NewSimulator(db)
	if err != nil {
		t.Fatal(err)
	}
	store, err := repository.NewIntents(db)
	if err != nil {
		t.Fatal(err)
	}
	return core, store
}

func intentServiceFixture(t *testing.T, store domain.IntentExecutionRepository, gateway domain.TradingGateway) domain.IntentService {
	t.Helper()
	service, err := usecase.NewIntentService(store, gateway)
	if err != nil {
		t.Fatal(err)
	}
	return service
}
