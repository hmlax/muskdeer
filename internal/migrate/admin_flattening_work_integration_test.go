//go:build integration

package migrate

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/ownerid"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminFlattenWorkClaimRevalidatesAndReplaysWithoutTradingEffects(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	admin, submit, receipt := flattenWorkFixture(ctx, t, db)
	store := flattenWorkStore(t, db)
	token := flattenWorkToken(submit.Input.Authority)
	before := publicationInstance(ctx, t, db, receipt.StrategyID)
	// A valid durable confirmation survives its actor's later logout. Work is
	// owned by the actual current strategy lease, never the old bearer token.
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx, admin.Token, admin.CSRF); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.Claim(ctx, token, receipt.ID)
	if err != nil || claimed.State != domain.AdminFlattenActive || claimed.Revision != 2 || claimed.FlatteningID != receipt.ID ||
		claimed.OwnerID != token.OwnerID || claimed.OwnerEpoch != token.Epoch || claimed.ClaimedAt.IsZero() {
		t.Fatal("actual current owner did not claim confirmed pending work", err)
	}
	stored := flattenTaskRow(ctx, t, db, receipt.ID)
	replayed, err := store.Claim(ctx, token, receipt.ID)
	if err != nil || replayed.ID != claimed.ID || replayed.Revision != claimed.Revision || !proto.Equal(stored, flattenTaskRow(ctx, t, db, receipt.ID)) {
		t.Fatal("same owner claim replay changed task identity or revision", err)
	}
	if !proto.Equal(before, publicationInstance(ctx, t, db, receipt.StrategyID)) {
		t.Fatal("work claim changed strategy runtime/configuration/target")
	}
	core, _ := executionFixture(t, db)
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "BTC", "10", "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
	assertPortfolioRowCount(ctx, t, db, &modelv1.OrderIntent{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyAction{}, 0)
	assertGatewayOrderCount(ctx, t, db, 0)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
}

func TestAdminFlattenWorkClaimRejectsStaleBalancesScopeAndLostAuthority(t *testing.T) {
	for _, fault := range []string{"balance", "portfolio", "risk", "lease", "runtime", "scope"} {
		t.Run(fault, func(t *testing.T) {
			db, _ := migrationDatabase(t)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			admin, submit, receipt := flattenWorkFixture(ctx, t, db)
			token := flattenWorkToken(submit.Input.Authority)
			before := flattenTaskRow(ctx, t, db, receipt.ID)
			var err error
			switch fault {
			case "balance":
				err = db.WithContext(ctx).Model(&modelv1.SimulatorBalance{}).Where("account_id = ? AND asset = ?", receipt.AccountID, "BTC").UpdateColumn("revision", receipt.ExpectedBaseRevision+1).Error
			case "portfolio":
				err = db.WithContext(ctx).Model(&modelv1.PortfolioAccount{}).Where("account_id = ?", receipt.AccountID).UpdateColumn("revision", receipt.ExpectedPortfolioRevision+1).Error
			case "risk":
				request := riskBlockRequest(receipt.StrategyID)
				request.Key = "flatten-work-new-risk-revision"
				request.ExpectedRevision = receipt.ExpectedRiskRevision
				_, err = riskControlFixture(t, db).Change(ctx, admin.Token, admin.CSRF, request)
			case "lease":
				err = db.WithContext(ctx).Model(&modelv1.StrategyLease{}).Where("strategy_instance_id = ?", receipt.StrategyID).UpdateColumn("expires_at", 0).Error
			case "runtime":
				leases, leaseErr := repository.NewLeases(db)
				if leaseErr != nil {
					t.Fatal(leaseErr)
				}
				if err := leases.Release(ctx, token); err != nil {
					t.Fatal(err)
				}
				owner, ownerErr := ownerid.New(ctx, "flatten-work-unclaimed-runtime")
				if ownerErr != nil {
					t.Fatal(ownerErr)
				}
				token, err = leases.Acquire(ctx, token.Key, owner, 30*time.Second)
			case "scope":
				token.Key.StrategyID++
			}
			if err != nil {
				t.Fatal("fault injection did not persist", err)
			}
			result, err := flattenWorkStore(t, db).Claim(ctx, token, receipt.ID)
			if err == nil || result.ID != 0 || !proto.Equal(before, flattenTaskRow(ctx, t, db, receipt.ID)) {
				t.Fatal("stale confirmation or lost authority advanced pending work", err)
			}
			assertGatewayOrderCount(ctx, t, db, 0)
		})
	}
}

func TestAdminFlattenWorkConcurrentClaimAndHigherEpochKeepOneTask(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, submit, receipt := flattenWorkFixture(ctx, t, db)
	store := flattenWorkStore(t, db)
	token := flattenWorkToken(submit.Input.Authority)
	start := make(chan struct{})
	results := make(chan domain.AdminFlattenProgress, 4)
	failures := make(chan error, 4)
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			<-start
			result, err := store.Claim(ctx, token, receipt.ID)
			results <- result
			failures <- err
		})
	}
	close(start)
	workers.Wait()
	var id int64
	for range 4 {
		result, err := <-results, <-failures
		if err != nil || result.ID <= 0 || result.Revision != 2 || id != 0 && result.ID != id {
			t.Fatal("same owner claims did not have one durable effect", err)
		}
		id = result.ID
	}
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := leases.Release(ctx, token); err != nil {
		t.Fatal(err)
	}
	owner, err := ownerid.New(ctx, "flatten-work-takeover")
	if err != nil {
		t.Fatal(err)
	}
	current, err := leases.Acquire(ctx, token.Key, owner, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	authority := submit.Input.Authority
	authority.OwnerID, authority.OwnerEpoch = current.OwnerID, current.Epoch
	setFlattenRuntimeOwner(ctx, t, db, authority)
	if _, err := store.Claim(ctx, token, receipt.ID); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatal("old owner reclaimed work after actual takeover", err)
	}
	taken, err := store.Claim(ctx, current, receipt.ID)
	if err != nil || taken.ID != id || taken.Revision != 3 || taken.OwnerID != current.OwnerID || taken.OwnerEpoch != current.Epoch {
		t.Fatal("new current owner lost original task or exact epoch", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAccountFlatteningTask{}, 1)
	assertGatewayOrderCount(ctx, t, db, 0)
}

func TestAdminFlattenWorkStorageAndCommitLeaseFailureReturnNothingAndRollBack(t *testing.T) {
	for _, fault := range []string{"storage", "lease"} {
		t.Run(fault, func(t *testing.T) {
			db, _ := migrationDatabase(t)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			_, submit, receipt := flattenWorkFixture(ctx, t, db)
			before := flattenTaskRow(ctx, t, db, receipt.ID)
			callback := "flatten-work-fault-" + fault
			var injected int64
			if err := db.Callback().Update().After("gorm:update").Register(callback, func(tx *gorm.DB) {
				if _, task := tx.Statement.Dest.(*modelv1.AdminAccountFlatteningTask); !task || tx.Error != nil {
					return
				}
				if fault == "storage" {
					injected++
					tx.Error = tx.AddError(errors.New("private flatten work failure"))
					return
				}
				updated := tx.WithContext(ctx).Session(&gorm.Session{NewDB: true}).Model(&modelv1.StrategyLease{}).
					Where("strategy_instance_id = ?", receipt.StrategyID).UpdateColumn("expires_at", 0)
				injected = updated.RowsAffected
				if updated.Error != nil {
					tx.Error = tx.AddError(updated.Error)
				}
			}); err != nil {
				t.Fatal(err)
			}
			store := flattenWorkStore(t, db)
			token := flattenWorkToken(submit.Input.Authority)
			got, err := store.Claim(ctx, token, receipt.ID)
			if err == nil || got.ID != 0 || injected != 1 || strings.Contains(err.Error(), "private") || !proto.Equal(before, flattenTaskRow(ctx, t, db, receipt.ID)) {
				t.Fatal("failed claim returned partial work or failed to roll back", err)
			}
			if fault == "lease" && !errors.Is(err, domain.ErrLeaseLost) {
				t.Fatal("commit-time lease loss was not classified as lost ownership", err)
			}
			if err := db.Callback().Update().Remove(callback); err != nil {
				t.Fatal(err)
			}
			if restored, err := store.Claim(ctx, token, receipt.ID); err != nil || restored.ID != before.Id || restored.Revision != 2 {
				t.Fatal("failed claim left expired authority or a partial task revision", err)
			}
			assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
			assertGatewayOrderCount(ctx, t, db, 0)
		})
	}
}

func TestAdminFlattenWorkCompetingHistoricalConfirmationsReturnAccountConflict(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, submit, receipt := flattenSchemaFixture(ctx, t, db)
	if err := db.WithContext(ctx).Model(&modelv1.AdminAccountFlattening{}).Create(receipt).Error; err != nil {
		t.Fatal(err)
	}
	// Older schemas can retain multiple valid confirmations. They must never
	// become simultaneous active work, even through this actual repository.
	assertFlattenSchemaKeysAndIdentity(ctx, t, db, receipt)
	setFlattenRuntimeOwner(ctx, t, db, submit.Input.Authority)
	var tasks []*modelv1.AdminAccountFlatteningTask
	if err := db.WithContext(ctx).Model(&modelv1.AdminAccountFlatteningTask{}).Order("id").Find(&tasks).Error; err != nil || len(tasks) != 2 {
		t.Fatal("competing historical fixture lacks its two original tasks", err)
	}
	store := flattenWorkStore(t, db)
	token := flattenWorkToken(submit.Input.Authority)
	if _, err := store.Claim(ctx, token, tasks[0].FlatteningId); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Claim(ctx, token, tasks[1].FlatteningId); !errors.Is(err, domain.ErrAdminConflict) || got.ID != 0 {
		t.Fatal("second historical confirmation bypassed the active account", err)
	}
	if !proto.Equal(tasks[1], flattenTaskRow(ctx, t, db, tasks[1].FlatteningId)) {
		t.Fatal("conflicting claim changed its original queued marker")
	}
	assertGatewayOrderCount(ctx, t, db, 0)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
}

func flattenWorkFixture(ctx context.Context, t *testing.T, db *gorm.DB) (domain.AdminLogin, domain.GatewaySubmit, domain.AdminFlattenReceipt) {
	t.Helper()
	admin, submit, request := flattenAcceptanceFixture(ctx, t, db)
	receipt, err := flattenAcceptanceStore(t, db).Request(ctx, admin.Token, admin.CSRF, request)
	if err != nil {
		t.Fatal(err)
	}
	setFlattenRuntimeOwner(ctx, t, db, submit.Input.Authority)
	return admin, submit, receipt
}

func flattenWorkStore(t *testing.T, db *gorm.DB) domain.AdminFlattenWork {
	t.Helper()
	store, err := repository.NewAdminFlattenWork(db)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func flattenWorkToken(authority domain.StrategyAuthority) domain.LeaseToken {
	return domain.LeaseToken{
		Key: domain.LeaseKey{
			Kind:       domain.LeaseKindStrategy,
			StrategyID: authority.StrategyID,
		},
		OwnerID: authority.OwnerID,
		Epoch:   authority.OwnerEpoch,
	}
}
