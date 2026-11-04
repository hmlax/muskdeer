//go:build integration

package migrate

import (
	"context"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	executionmodelv1 "github.com/hmlax/muskdeer/deer/proto/execution/model/v1"
	strategymodelv1 "github.com/hmlax/muskdeer/deer/proto/strategy/model/v1"
)

func TestAdminFlatteningProgressWaitsForStrategyBeforeTakingLease(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, submit, receipt := flattenSchemaFixture(ctx, t, db)
	if err := db.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlattening{}).Create(receipt).Error; err != nil {
		t.Fatal(err)
	}
	setFlattenRuntimeOwner(ctx, t, db, submit.Input.Authority)
	claim := flattenTaskClaim(flattenTaskRow(ctx, t, db, receipt.Id), submit.Input.Authority)
	process := make(chan int64, 1)
	finished := make(chan error, 1)
	var workers sync.WaitGroup
	holderErr := db.WithContext(ctx).Model(&strategymodelv1.StrategyInstance{}).Transaction(func(holder *gorm.DB) error {
		var strategy strategymodelv1.StrategyInstance
		if err := holder.WithContext(ctx).Model(&strategymodelv1.StrategyInstance{}).Where("id = ?", receipt.StrategyInstanceId).
			Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).Take(&strategy).Error; err != nil {
			return err
		}
		workers.Go(func() {
			err := db.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlatteningTask{}).Transaction(func(tx *gorm.DB) error {
				if err := updateFlattenTaskClaim(ctx, tx, claim); err != nil {
					return err
				}
				var backend struct{ ProcessID int64 }
				if err := tx.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlatteningTask{}).Where("id = ?", claim.Id).
					Select("pg_backend_pid() AS process_id").Take(&backend).Error; err != nil {
					return err
				}
				process <- backend.ProcessID
				return nil // Actual COMMIT now waits on the held strategy row.
			})
			finished <- err
		})
		var pid int64
		select {
		case pid = <-process:
		case err := <-finished:
			finished <- err
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
		if err := waitFlattenClaimBlocked(ctx, db, claim.Id, pid); err != nil {
			return err
		}
		// A final sender already holding the strategy mutex must be able to
		// take its lease now. A reversed claim order has already locked it.
		var lease strategymodelv1.StrategyLease
		return holder.WithContext(ctx).Model(&strategymodelv1.StrategyLease{}).Where("strategy_instance_id = ?", receipt.StrategyInstanceId).
			Clauses(clause.Locking{
				Strength: "UPDATE",
				Options:  "NOWAIT",
			}).Take(&lease).Error
	})
	workers.Wait()
	if holderErr != nil {
		t.Fatal("claim took a lease before waiting for the existing strategy mutex", holderErr)
	}
	if err := <-finished; err != nil {
		t.Fatal("claim did not complete after the strategy mutex was released", err)
	}
	assertGatewayOrderCount(ctx, t, db, 0)
}

func waitFlattenClaimBlocked(ctx context.Context, db *gorm.DB, taskID, pid int64) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var observation struct{ Blockers int64 }
		if err := db.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlatteningTask{}).Where("id = ?", taskID).
			Select("cardinality(pg_blocking_pids(?)) AS blockers", pid).Take(&observation).Error; err != nil {
			return err
		}
		if observation.Blockers > 0 {
			return nil
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
