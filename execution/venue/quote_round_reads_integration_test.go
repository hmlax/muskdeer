//go:build integration

package venue

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

// countTableReads counts SELECTs per table on this runtime's database handle.
func countTableReads(t *testing.T, db *gorm.DB, name string) map[string]*atomic.Int64 {
	t.Helper()
	counts := map[string]*atomic.Int64{"trading_account_policies": {}, "strategy_instances": {}}
	if err := db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil {
			return
		}
		if counter := counts[tx.Statement.Schema.Table]; counter != nil {
			counter.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	return counts
}

// The round's writer holds the fund gate shared with account policy updates
// and account disabling, so the round-start policy row cannot change before
// these sends. Each candidate still checks the aggregate of all siblings.
func TestMakerRoundReusesGatedAccountPolicyForEveryCandidate(t *testing.T) {
	runtime, cycle, client, setup := batchQuoteSetup(t.Context(), t, 16)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	budget, err := runtime.store.Budget(ctx, setup.budget.AccountId)
	if err != nil {
		t.Fatal(err)
	}
	cycle.budget = budget
	reads := countTableReads(t, runtime.db, "count_round_reads")
	close(client.release)
	if err := runtime.fillQuoteDepth(ctx, cycle); err != nil {
		t.Fatal(err)
	}
	if cycle.result.Placed != 16 || reads["trading_account_policies"].Load() != 0 {
		t.Fatalf("placed=%d policy reads=%d, want 16 sends using the round's gated policy", cycle.result.Placed, reads["trading_account_policies"].Load())
	}
}

func TestMakerRoundPolicyStillLimitsAggregateSiblings(t *testing.T) {
	runtime, cycle, client, setup := batchQuoteSetup(t.Context(), t, 4)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	close(client.release)
	if err := setup.db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Where("id = ?", setup.budget.Id).Update("maximum_notional", "130").Error; err != nil {
		t.Fatal(err)
	}
	budget, err := runtime.store.Budget(ctx, setup.budget.AccountId)
	if err != nil {
		t.Fatal(err)
	}
	cycle.budget = budget
	if err := runtime.fillQuoteDepth(ctx, cycle); !errors.Is(err, ErrRiskLimit) || client.calls.Load() != 0 {
		t.Fatalf("oversized wave error=%v dispatched=%d, want risk refusal before any send", err, client.calls.Load())
	}
}

func TestMakerRoundDisabledPolicyRefusesBeforeSending(t *testing.T) {
	runtime, cycle, client, setup := batchQuoteSetup(t.Context(), t, 4)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	close(client.release)
	budget, err := runtime.store.Budget(ctx, setup.budget.AccountId)
	if err != nil {
		t.Fatal(err)
	}
	budget.Enabled = false
	cycle.budget = budget
	if err := runtime.fillQuoteDepth(ctx, cycle); !errors.Is(err, ErrRiskLimit) || client.calls.Load() != 0 {
		t.Fatalf("disabled policy error=%v dispatched=%d", err, client.calls.Load())
	}
}

// Instance stop is not serialized by the fund gate: every wave re-reads the
// current configuration immediately before dispatching, once for all siblings.
func TestMakerWaveChecksCurrentConfigurationOncePerWave(t *testing.T) {
	runtime, cycle, client, _ := batchQuoteSetup(t.Context(), t, 16)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	reads := countTableReads(t, runtime.db, "count_wave_reads")
	close(client.release)
	if err := runtime.fillQuoteDepth(ctx, cycle); err != nil {
		t.Fatal(err)
	}
	if cycle.result.Placed != 16 || reads["strategy_instances"].Load() != 2 {
		t.Fatalf("placed=%d config reads=%d, want one current configuration read per 8-order wave", cycle.result.Placed, reads["strategy_instances"].Load())
	}
}

func TestMakerWaveStoppedInstanceSendsNothing(t *testing.T) {
	runtime, cycle, client, setup := batchQuoteSetup(t.Context(), t, 8)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	close(client.release)
	if _, err := instanceconfig.Manage(ctx, setup.db, instanceconfig.ManagementWrite{
		Operation:              instanceconfig.ManagementStop,
		Subject:                "maker-wave-test",
		RequestID:              "maker-wave-test-stop",
		ID:                     setup.instance.Id,
		ExpectedRevision:       setup.instance.Revision,
		ExpectedControlVersion: setup.instance.RequestedConfigVersion,
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := runtime.fillQuoteDepth(ctx, cycle); !errors.Is(err, native.ErrInvalid) || client.calls.Load() != 0 || cycle.result.Placed != 0 {
		t.Fatalf("stopped instance err=%v dispatched=%d placed=%d", err, client.calls.Load(), cycle.result.Placed)
	}
}
