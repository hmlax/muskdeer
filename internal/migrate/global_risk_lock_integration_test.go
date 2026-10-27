//go:build integration

package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type globalReleaseProbeKey struct{}

type globalCoreResult struct {
	order domain.CoreOrder
	err   error
}

func TestFinalGlobalGateDoesNotDeadlockAnotherIntentRelease(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	intents, submit := admissionFixture(ctx, t, db)
	core, store := executionFixture(t, db)
	other := submit.Input
	other.IdempotencyKey = "other-pending-release"
	other.Quantity = decimal.RequireFromString("0.5")
	pending, err := intents.Accept(ctx, other, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	feed, _, _, _ := admissionSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	prepared, err := store.Accept(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err = store.Prepare(ctx, prepared, submit.Input.Authority, submit.ExecutionAuthority, submit.Input.Deadline)
	if err != nil {
		t.Fatal(err)
	}
	commandInput := prepared.Input
	commandInput.IdempotencyKey = domain.CoreSubmitKeyPrefix + prepared.ClientOrderID
	command := domain.GatewaySubmit{
		IntentID:           prepared.ID,
		Input:              commandInput,
		ClientOrderID:      prepared.ClientOrderID,
		ExecutionAuthority: prepared.ExecutionAuthority,
	}
	installGlobalCommitCheckpoint(ctx, t, db, feed.ID)
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	// The controlled schedule owns a gate, core, release and observer connection.
	pool.SetMaxOpenConns(6)
	var workers sync.WaitGroup
	releaseGate := make(chan struct{})
	var once sync.Once
	corePID, releasePID := make(chan int, 1), make(chan int, 1)
	const createHook = "muskdeer:test:global-core-pid"
	const queryHook = "muskdeer:test:global-release-pid"
	t.Cleanup(func() {
		once.Do(func() { close(releaseGate) })
		cancel()
		workers.Wait()
		if err := db.Callback().Create().Remove(createHook); err != nil {
			t.Error(err)
		}
		if err := db.Callback().Query().Remove(queryHook); err != nil {
			t.Error(err)
		}
	})
	if err := db.Callback().Create().After("gorm:create").Register(createHook, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*modelv1.SimulatorOrder); !ok {
			return
		}
		sendBackendPID(tx, corePID)
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().Before("gorm:query").Register(queryHook, func(tx *gorm.DB) {
		if tx.Statement.Context.Value(globalReleaseProbeKey{}) != true {
			return
		}
		if _, ok := tx.Statement.Dest.(*modelv1.AccountBudget); !ok {
			return
		}
		sendBackendPID(tx, releasePID)
	}); err != nil {
		t.Fatal(err)
	}
	gateReady, gateResult := make(chan struct{}, 1), make(chan error, 1)
	workers.Go(func() {
		gateResult <- db.WithContext(ctx).Model(&modelv1.MarketFeed{}).Transaction(func(tx *gorm.DB) error {
			var row modelv1.MarketFeed
			if err := tx.WithContext(ctx).Model(&modelv1.MarketFeed{}).Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, feed.ID).Error; err != nil {
				return err
			}
			gateReady <- struct{}{}
			select {
			case <-releaseGate:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	})
	select {
	case <-gateReady:
	case <-ctx.Done():
		t.Fatal("checkpoint gate did not start")
	}
	submitted := make(chan globalCoreResult, 1)
	workers.Go(func() {
		order, err := core.Submit(ctx, command)
		submitted <- globalCoreResult{
			order: order,
			err:   err,
		}
	})
	select {
	case pid := <-corePID:
		waitBackendLock(ctx, t, db, pid)
	case result := <-submitted:
		t.Fatalf("core ended before the controlled commit boundary: %+v, %v", result.order, errors.Unwrap(result.err))
	case <-ctx.Done():
		t.Fatal("core never entered the controlled commit boundary")
	}
	released := make(chan error, 1)
	workers.Go(func() {
		_, err := store.Reject(context.WithValue(ctx, globalReleaseProbeKey{}, true), pending, domain.IntentErrorRiskDenied)
		released <- err
	})
	waitBackendLock(ctx, t, db, receiveProbePID(ctx, t, releasePID))
	once.Do(func() { close(releaseGate) })
	for _, result := range []<-chan error{gateResult, released} {
		select {
		case err := <-result:
			if err != nil {
				t.Fatalf("final global gate/release lock cycle: %v; cause: %v", err, errors.Unwrap(err))
			}
		case <-ctx.Done():
			t.Fatal("final global gate/release did not join")
		}
	}
	select {
	case result := <-submitted:
		if result.err != nil {
			t.Fatalf("core final commit failed: %v; cause: %v", result.err, errors.Unwrap(result.err))
		}
		if _, err := store.Confirm(ctx, prepared, result.order); err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("core final commit did not join")
	}
	assertGatewayOrderCount(ctx, t, db, 1)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
}

func sendBackendPID(tx *gorm.DB, target chan<- int) {
	var pid int
	err := tx.WithContext(tx.Statement.Context).Session(&gorm.Session{NewDB: true}).Model(&modelv1.SchemaMigration{}).Where("version = ?", 1).Select("pg_backend_pid()").Scan(&pid).Error
	if err != nil {
		tx.Error = errors.Join(tx.Error, err)
		return
	}
	select {
	case target <- pid:
	default:
	}
}

func receiveProbePID(ctx context.Context, t *testing.T, source <-chan int) int {
	t.Helper()
	select {
	case pid := <-source:
		return pid
	case <-ctx.Done():
		t.Fatal("owned SQL transaction did not expose its backend")
		return 0
	}
}

func waitBackendLock(ctx context.Context, t *testing.T, db *gorm.DB, pid int) {
	t.Helper()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		var state struct{ Waiting bool }
		// Only PostgreSQL diagnostics for this test's explicitly captured backend.
		err := db.WithContext(ctx).Model(&modelv1.SchemaMigration{}).Where("version = ?", 1).Select("EXISTS (SELECT 1 FROM pg_stat_activity WHERE pid = ? AND wait_event_type = 'Lock') AS waiting", pid).Take(&state).Error
		if err != nil {
			t.Fatal(err)
		}
		if state.Waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("owned SQL backend did not reach the controlled lock boundary")
		case <-tick.C:
		}
	}
}

func installGlobalCommitCheckpoint(ctx context.Context, t *testing.T, db *gorm.DB, feedID int64) {
	t.Helper()
	definitions, err := loadDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	sql := fmt.Sprintf(`CREATE FUNCTION wait_global_risk_test_gate() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM 1 FROM market_feeds WHERE id = %d FOR UPDATE;
    RETURN NEW;
END;
$$;
CREATE CONSTRAINT TRIGGER simulator_global_risk_pause AFTER INSERT ON simulator_commands
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION wait_global_risk_test_gate();`, feedID)
	hash := sha256.Sum256([]byte(sql))
	definitions = append(definitions, definition{
		version:  int64(len(definitions) + 1),
		name:     "test-global-commit-checkpoint.sql",
		sql:      sql,
		checksum: hex.EncodeToString(hash[:]),
	})
	if err := applyDefinitions(ctx, db, definitions); err != nil {
		t.Fatal(err)
	}
}
