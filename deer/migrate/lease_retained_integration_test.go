//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hmlax/muskdeer/deer/ownerid"
	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/repository"
	"gorm.io/gorm/clause"
)

func TestRetainedLeaseAcquireReadsWithoutWaitingForEffectRowLock(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	store, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := ownerid.New(ctx, "retained-owner")
	if err != nil {
		t.Fatal(err)
	}
	key := domain.LeaseKey{
		Kind:      domain.LeaseKindExecution,
		AccountID: "retained-fund",
	}
	first, err := store.Acquire(ctx, key, owner, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	tx := db.WithContext(ctx).Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	var row model.ExecutionShardLease
	if err := tx.Model(&model.ExecutionShardLease{}).Where("account_id = ?", key.AccountID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	read, stop := context.WithTimeout(ctx, 200*time.Millisecond)
	defer stop()
	again, err := store.Acquire(read, key, owner, 10*time.Second)
	if err != nil || again.Epoch != first.Epoch || again.OwnerID != first.OwnerID || !again.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatalf("retained Acquire blocked or changed authority: %v, %v", again, err)
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	if err := store.Release(ctx, first); err != nil {
		t.Fatal(err)
	}
	next, err := store.Acquire(ctx, key, owner, 10*time.Second)
	if err != nil || next.Epoch != first.Epoch+1 {
		t.Fatalf("released epoch reused: %v, %v", next, err)
	}
	if _, err := store.Renew(ctx, first, 10*time.Second); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("stale epoch renewed: %v", err)
	}
}
