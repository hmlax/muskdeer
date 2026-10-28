//go:build integration

package migrate

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/usecase"
)

func TestSimulatorUnsentCancellationResolvesOnlyItsExpiredCommandAndKeepsLiveFunds(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	submit.Input.Price, submit.Input.Quantity = decimal.NewFromInt(99), decimal.NewFromInt(1)
	live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil || live.State != domain.OrderStateLive {
		t.Fatal("live parent fixture failed")
	}
	requests, err := repository.NewSimulatorCancellations(db)
	if err != nil {
		t.Fatal(err)
	}
	input := cancelInputFixture(ctx, submit, live.ClientOrderID, "unprepared-cancellation")
	input.Deadline = time.Now().Add(time.Second)
	accepted, err := requests.Accept(ctx, input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	cancels, err := usecase.NewCancellationService(requests, intents, core)
	if err != nil {
		t.Fatal(err)
	}
	before, err := cancels.RecoverByKey(ctx, input.AccountID, input.IdempotencyKey)
	if err != nil || before.Request.State != domain.CancellationRequested {
		t.Fatal("unexpired cancellation was resolved without a core effect")
	}
	time.Sleep(max(time.Until(input.Deadline.Add(30*time.Millisecond)), time.Millisecond))
	external := cancellationServiceFixture(t, db, intents, core)
	unknown, err := external.RecoverByKey(ctx, input.AccountID, input.IdempotencyKey)
	if err != nil || unknown.Request.State != domain.CancellationRequested {
		t.Fatal("generic cancellation service assumed local core authority")
	}
	resolved, err := cancels.RecoverByKey(ctx, input.AccountID, input.IdempotencyKey)
	if err != nil || resolved.Request.State != domain.CancellationRejected || resolved.Request.ErrorCode != domain.IntentErrorCoreNotAccepted || resolved.Order.State != domain.OrderStateLive || resolved.Order.CancelState != domain.CancelStateNone {
		t.Fatalf("expired absent cancellation did not preserve its live parent: %+v, %v", resolved, err)
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "99")
	assertCoreBalance(ctx, t, core, input.AccountID, "USDT", "900.901", "99.099")
	input.Deadline = time.Now().Add(time.Second)
	again, err := requests.Accept(ctx, input, submit.ExecutionAuthority)
	if err != nil || again.State != domain.CancellationRejected || !again.Input.Deadline.Equal(accepted.Input.Deadline) {
		t.Fatal("resolved cancellation identity was renewed")
	}
	oldCore := input
	oldCore.IdempotencyKey = resolved.Request.CoreCommandKey
	_, err = core.Cancel(ctx, domain.GatewayCancel{
		IntentID:           live.ID,
		Input:              oldCore,
		ExecutionAuthority: submit.ExecutionAuthority,
	})
	assertDatabaseCode(t, err, "23514")
	if _, err := requests.Observe(ctx, resolved.Request, domain.CancellationRejected, domain.IntentErrorCoreNotAccepted); !errors.Is(err, domain.ErrInvalidOrder) {
		t.Fatal("ordinary observation bypassed no-effect proof")
	}
	input.IdempotencyKey = "fresh-cancellation-after-definitive-no-effect"
	fresh, err := cancels.Cancel(ctx, input, submit.ExecutionAuthority)
	if err != nil || fresh.Request.State != domain.CancellationPending {
		t.Fatalf("new decision could not request real cancellation: %+v, %v", fresh, err)
	}
	if _, err := core.ConfirmCancel(ctx, input.AccountID, input.ClientOrderID); err != nil {
		t.Fatal(err)
	}
	confirmed, err := cancels.RecoverByKey(ctx, input.AccountID, input.IdempotencyKey)
	if err != nil || confirmed.Request.State != domain.CancellationConfirmed || confirmed.Order.State != domain.OrderStateCanceled {
		t.Fatal("new cancellation did not confirm its actual core effect")
	}
	err = db.WithContext(ctx).Model(&modelv1.CancellationRequest{}).Where("id = ?", resolved.Request.ID).Delete(&modelv1.CancellationRequest{}).Error
	assertDatabaseCode(t, err, "23514")
	assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
	assertCoreBalance(ctx, t, core, input.AccountID, "USDT", "1000", "0")
	assertGatewayOrderCount(ctx, t, db, 1)
}

func TestSimulatorCancellationResolutionKeepsCommittedCommandAndRejectsSnapshotProof(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	requests, err := repository.NewSimulatorCancellations(db)
	if err != nil {
		t.Fatal(err)
	}
	input := cancelInputFixture(ctx, submit, live.ClientOrderID, "committed-cancellation-proof")
	input.Deadline = time.Now().Add(time.Second)
	accepted, err := requests.Accept(ctx, input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	err = db.WithContext(ctx).Model(&modelv1.CancellationRequest{}).Where("id = ?", accepted.ID).UpdateColumn("core_command_key", domain.CoreCancelKeyPrefix+"forged").Error
	assertDatabaseCode(t, err, "23514")
	prepared, err := requests.Prepare(ctx, accepted)
	if err != nil {
		t.Fatal(err)
	}
	forge := func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&modelv1.CancellationRequest{}).Where("id = ?", prepared.ID).
			Select("State", "ErrorCode", "Revision").Updates(&modelv1.CancellationRequest{
			State:     string(domain.CancellationRejected),
			ErrorCode: domain.IntentErrorCoreNotAccepted,
			Revision:  prepared.Revision + 1,
		}).Error
	}
	err = db.WithContext(ctx).Model(&modelv1.CancellationRequest{}).Transaction(forge)
	assertDatabaseCode(t, err, "23514")
	commandInput := input
	commandInput.IdempotencyKey = prepared.CoreCommandKey
	if _, err := core.Cancel(ctx, domain.GatewayCancel{
		IntentID:           live.ID,
		Input:              commandInput,
		ExecutionAuthority: submit.ExecutionAuthority,
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(max(time.Until(input.Deadline.Add(30*time.Millisecond)), time.Millisecond))
	err = db.WithContext(ctx).Model(&modelv1.CancellationRequest{}).Transaction(forge, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	assertDatabaseCode(t, err, "23514")
	err = db.WithContext(ctx).Model(&modelv1.CancellationRequest{}).Transaction(forge)
	assertDatabaseCode(t, err, "23514")
	unknown, err := requests.Observe(ctx, prepared, domain.CancellationUnknown, domain.IntentErrorOutcomeUnknown)
	if err != nil || unknown.State != domain.CancellationUnknown || unknown.ErrorCode == domain.IntentErrorCoreNotAccepted {
		t.Fatal("committed original cancel command was declared absent")
	}
	service, err := usecase.NewCancellationService(requests, intents, core)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := service.RecoverByKey(ctx, input.AccountID, input.IdempotencyKey)
	if err != nil || pending.Request.State != domain.CancellationPending || pending.Order.CancelState != domain.CancelStatePending {
		t.Fatal("actual pending core effect was not recovered")
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertCoreBalance(ctx, t, core, input.AccountID, "USDT", "799.8", "200.2")
}

func TestSimulatorCoreAdmissionRejectsRepeatableSnapshotBeforeAnyIntentLookup(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, _ := executionFixture(t, db)
	err := db.WithContext(ctx).Model(&modelv1.SimulatorCommand{}).Transaction(func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&modelv1.SimulatorCommand{}).Create(&modelv1.SimulatorCommand{
			AccountId:          submit.Input.AccountID,
			CommandKey:         submit.Input.IdempotencyKey,
			Action:             domain.GatewayActionSubmit,
			ClientOrderId:      submit.ClientOrderID,
			StrategyInstanceId: submit.Input.Authority.StrategyID,
			ConfigVersion:      submit.Input.Authority.ConfigVersion,
			OwnerId:            submit.Input.Authority.OwnerID,
			OwnerEpoch:         submit.Input.Authority.OwnerEpoch,
			ExecutionOwnerId:   submit.ExecutionAuthority.OwnerID,
			ExecutionEpoch:     submit.ExecutionAuthority.Epoch,
			InstrumentId:       submit.Input.InstrumentID,
			Fingerprint:        strings.Repeat("a", 64),
			DeadlineAt:         submit.Input.Deadline.UnixMilli(),
		}).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	assertDatabaseCode(t, err, "23514")
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.ConstraintName != "simulator_submission_resolution" {
		t.Fatal("repeatable snapshot was rejected outside the intended admission gate")
	}
	assertGatewayOrderCount(ctx, t, db, 0)
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
}

func assertCancellationResolutionBudgets(ctx context.Context, t *testing.T, db *gorm.DB, submit domain.GatewaySubmit, expected string) {
	t.Helper()
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, expected)
	value := decimal.RequireFromString(expected)
	var strategy modelv1.StrategyBudget
	if err := db.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Where("strategy_instance_id = ?", submit.Input.Authority.StrategyID).First(&strategy).Error; err != nil {
		t.Fatal(err)
	}
	if !decimal.RequireFromString(strategy.ReservedNotional).Equal(value) {
		t.Fatal("cancellation resolution changed strategy reservation")
	}
	var global []*modelv1.RiskBudget
	if err := db.WithContext(ctx).Model(&modelv1.RiskBudget{}).Find(&global).Error; err != nil {
		t.Fatal(err)
	}
	if len(global) != 2 {
		t.Fatal("platform/instrument budget evidence is incomplete")
	}
	for _, budget := range global {
		if !decimal.RequireFromString(budget.ReservedNotional).Equal(value) {
			t.Fatal("cancellation resolution changed global reservation")
		}
	}
}
