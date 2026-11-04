//go:build integration

package migrate

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/ownerid"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/proto/execution/model/v1"
	strategymodelv1 "github.com/hmlax/muskdeer/deer/proto/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/repository"
)

func TestAdminFlatteningProgressInitializesAtomicallyAndOnlyObservesCurrentViewer(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	admin, submit, request := flattenAcceptanceFixture(ctx, t, db)
	store := flattenAcceptanceStore(t, db)
	var rolledBackID int64
	var observedTasks int64
	const callback = "flatten-progress-fail-after-initialize"
	if err := db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
		if receipt, ok := tx.Statement.Dest.(*executionmodelv1.AdminAccountFlattening); ok {
			rolledBackID = receipt.Id
			err := tx.WithContext(ctx).Session(&gorm.Session{NewDB: true}).Model(&executionmodelv1.AdminAccountFlatteningTask{}).
				Where("flattening_id = ?", receipt.Id).Count(&observedTasks).Error
			tx.Error = tx.AddError(errors.Join(err, errors.New("private fault after automatic queue creation")))
		}
	}); err != nil {
		t.Fatal(err)
	}
	failed, err := store.Request(ctx, admin.Token, admin.CSRF, request)
	if err == nil || failed.ID != 0 || rolledBackID <= 0 || observedTasks != 1 {
		t.Fatal("queue atomicity fixture did not reach its rollback point", err)
	}
	if err := db.Callback().Create().Remove(callback); err != nil {
		t.Fatal(err)
	}
	assertPortfolioRowCount(ctx, t, db, &executionmodelv1.AdminAccountFlatteningTask{}, 0)
	assertPortfolioRowCount(ctx, t, db, &executionmodelv1.AdminAccountFlattening{}, 0)
	assertFlatAuditCount(ctx, t, db, 0)
	receipt, err := store.Request(ctx, admin.Token, admin.CSRF, request)
	if err != nil {
		t.Fatal(err)
	}
	viewer := riskActorFixture(ctx, t, db, "flatten-progress-viewer", domain.AdminRoleViewer, 231, domain.AdminSessionTTL)
	before := flattenTaskRow(ctx, t, db, receipt.ID)
	progress, err := store.Progress(ctx, viewer.Token, request.StrategyID, receipt.ID)
	if err != nil || progress.ID != before.Id || progress.FlatteningID != receipt.ID || progress.AccountID != request.AccountID ||
		progress.State != domain.AdminFlattenQueued || progress.Revision != 1 || progress.OwnerEpoch != 0 || progress.OwnerID != "" || !progress.ClaimedAt.IsZero() {
		t.Fatal("current viewer did not observe unclaimed queued work", err)
	}
	if _, err := store.Request(ctx, admin.Token, admin.CSRF, request); err != nil {
		t.Fatal("receipt replay failed after task initialization", err)
	}
	if !proto.Equal(before, flattenTaskRow(ctx, t, db, receipt.ID)) {
		t.Fatal("read or original-key replay claimed or rewrote durable work")
	}
	if _, err := store.Progress(ctx, viewer.Token, request.StrategyID+1, receipt.ID); !errors.Is(err, domain.ErrAdminNotFound) {
		t.Fatal("progress crossed the requested strategy scope", err)
	}
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx, viewer.Token, viewer.CSRF); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Progress(ctx, viewer.Token, request.StrategyID, receipt.ID); !errors.Is(err, domain.ErrUnauthenticated) || got.ID != 0 {
		t.Fatal("revoked viewer read durable work", err)
	}
	assertPortfolioRowCount(ctx, t, db, &executionmodelv1.AdminAccountFlatteningTask{}, 1)
	assertGatewayOrderCount(ctx, t, db, 0)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
}

func TestAdminFlatteningProgressRejectsRewritesForgedOwnerAndInventedCompletion(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, submit, receipt := flattenSchemaFixture(ctx, t, db)
	if err := db.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlattening{}).Create(receipt).Error; err != nil {
		t.Fatal(err)
	}
	row := flattenTaskRow(ctx, t, db, receipt.Id)
	forgedInsert := flattenTaskClaim(row, submit.Input.Authority)
	forgedInsert.Id = 0
	assertEmergencySchemaConstraint(t, db.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlatteningTask{}).Create(forgedInsert).Error, "admin_flatten_task_initial")
	for _, mutate := range []func(*executionmodelv1.AdminAccountFlatteningTask){
		func(r *executionmodelv1.AdminAccountFlatteningTask) { r.State = "SUCCEEDED" },
		func(r *executionmodelv1.AdminAccountFlatteningTask) { r.State = string(domain.AdminFlattenActive) },
		func(r *executionmodelv1.AdminAccountFlatteningTask) { r.AccountId += "-other" },
		func(r *executionmodelv1.AdminAccountFlatteningTask) { r.InstrumentId++ },
		func(r *executionmodelv1.AdminAccountFlatteningTask) { r.FlatteningId++ },
		func(r *executionmodelv1.AdminAccountFlatteningTask) { r.CreateAt++ },
		func(r *executionmodelv1.AdminAccountFlatteningTask) { r.DeleteAt = proto.Int64(time.Now().UnixMilli()) },
	} {
		candidate := proto.CloneOf(row)
		candidate.Revision++
		mutate(candidate)
		assertDatabaseCode(t, db.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlatteningTask{}).Where("id = ?", row.Id).
			Select("State", "Revision", "AccountId", "InstrumentId", "FlatteningId", "CreateAt", "DeleteAt").Updates(candidate).Error, "23514")
	}
	assertDatabaseCode(t, db.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlatteningTask{}).Where("id = ?", row.Id).Delete(&executionmodelv1.AdminAccountFlatteningTask{}).Error, "23514")
	claim := flattenTaskClaim(row, submit.Input.Authority)
	claim.OwnerEpoch = proto.Int64(submit.Input.Authority.OwnerEpoch + 1)
	assertEmergencySchemaConstraint(t, updateFlattenTaskClaim(ctx, db, claim), "admin_flatten_task_owner")
	if !proto.Equal(row, flattenTaskRow(ctx, t, db, receipt.Id)) {
		t.Fatal("rejected work mutation changed its original pending marker")
	}
	assertGatewayOrderCount(ctx, t, db, 0)
}

func TestAdminFlatteningProgressAllowsOnlyOneActiveTaskPerAccountUnderConcurrency(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, submit, receipt := flattenSchemaFixture(ctx, t, db)
	if err := db.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlattening{}).Create(receipt).Error; err != nil {
		t.Fatal(err)
	}
	assertFlattenSchemaKeysAndIdentity(ctx, t, db, receipt)
	setFlattenRuntimeOwner(ctx, t, db, submit.Input.Authority)
	var rows []*executionmodelv1.AdminAccountFlatteningTask
	if err := db.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlatteningTask{}).Order("id").Find(&rows).Error; err != nil || len(rows) != 2 {
		t.Fatal("independent confirmations did not get distinct queued markers", err)
	}
	results := make(chan error, 2)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for _, row := range rows {
		claim := flattenTaskClaim(row, submit.Input.Authority)
		workers.Go(func() {
			<-start
			results <- updateFlattenTaskClaim(ctx, db, claim)
		})
	}
	close(start)
	workers.Wait()
	winners, conflicts := 0, 0
	for range 2 {
		err := <-results
		var databaseError *pgconn.PgError
		switch {
		case err == nil:
			winners++
		case errors.As(err, &databaseError) && databaseError.Code == "23505" && databaseError.ConstraintName == "uk_admin_flatten_task_active_account":
			conflicts++
		default:
			t.Fatal("concurrent activation failed for an unexpected reason", err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatal("account allowed multiple active flatten tasks", winners, conflicts)
	}
	var active int64
	if err := db.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlatteningTask{}).Where("state = ?", domain.AdminFlattenActive).Count(&active).Error; err != nil || active != 1 {
		t.Fatal("durable active work uniqueness was lost", err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertGatewayOrderCount(ctx, t, db, 0)
}

func TestAdminFlatteningProgressRechecksOwnerAtCommitAndRollsBackExpiredClaim(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, submit, receipt := flattenSchemaFixture(ctx, t, db)
	if err := db.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlattening{}).Create(receipt).Error; err != nil {
		t.Fatal(err)
	}
	setFlattenRuntimeOwner(ctx, t, db, submit.Input.Authority)
	before := flattenTaskRow(ctx, t, db, receipt.Id)
	future := flattenTaskClaim(before, submit.Input.Authority)
	future.ClaimedAt = proto.Int64(before.CreateAt + time.Minute.Milliseconds())
	assertEmergencySchemaConstraint(t, updateFlattenTaskClaim(ctx, db, future), "admin_flatten_task_clock")
	err := db.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlatteningTask{}).Transaction(func(tx *gorm.DB) error {
		if err := updateFlattenTaskClaim(ctx, tx, flattenTaskClaim(before, submit.Input.Authority)); err != nil {
			return err
		}
		// An actual persisted lease expires after the update but before COMMIT.
		return tx.WithContext(ctx).Model(&strategymodelv1.StrategyLease{}).Where("strategy_instance_id = ?", receipt.StrategyInstanceId).
			UpdateColumn("expires_at", 0).Error
	})
	assertEmergencySchemaConstraint(t, err, "admin_flatten_task_owner")
	if !proto.Equal(before, flattenTaskRow(ctx, t, db, receipt.Id)) {
		t.Fatal("expired claim left a partial active marker")
	}
	if err := updateFlattenTaskClaim(ctx, db, flattenTaskClaim(before, submit.Input.Authority)); err != nil {
		var lease strategymodelv1.StrategyLease
		var clock struct{ NowMS int64 }
		leaseErr := db.WithContext(ctx).Model(&strategymodelv1.StrategyLease{}).Where("strategy_instance_id = ?", receipt.StrategyInstanceId).Take(&lease).Error
		clockErr := db.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlatteningTask{}).Where("id = ?", before.Id).
			Select("floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint AS now_ms").Take(&clock).Error
		instance := publicationInstance(ctx, t, db, receipt.StrategyInstanceId)
		t.Log("claim diagnostics", "lease_error", leaseErr, "clock_error", clockErr,
			"lease_owner_matches", lease.OwnerId == submit.Input.Authority.OwnerID, "lease_epoch", lease.Epoch, "lease_remaining_ms", lease.ExpiresAt-clock.NowMS,
			"runtime_owner_matches", instance.RuntimeOwnerId == submit.Input.Authority.OwnerID, "runtime_epoch", instance.RuntimeOwnerEpoch,
			"host_clock_ahead_ms", time.Now().UnixMilli()-clock.NowMS)
		t.Fatal("rolled-back expiration or activation could not recover", err)
	}
	active := flattenTaskRow(ctx, t, db, receipt.Id)
	if active.State != string(domain.AdminFlattenActive) || active.Revision != 2 || active.GetOwnerEpoch() != submit.Input.Authority.OwnerEpoch {
		t.Fatal("current owner did not retain its durable claim")
	}
	assertGatewayOrderCount(ctx, t, db, 0)
}

func TestAdminFlatteningProgressRetainsOriginalWorkAcrossOwnerTakeover(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, submit, receipt := flattenSchemaFixture(ctx, t, db)
	if err := db.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlattening{}).Create(receipt).Error; err != nil {
		t.Fatal(err)
	}
	setFlattenRuntimeOwner(ctx, t, db, submit.Input.Authority)
	queued := flattenTaskRow(ctx, t, db, receipt.Id)
	if err := updateFlattenTaskClaim(ctx, db, flattenTaskClaim(queued, submit.Input.Authority)); err != nil {
		t.Fatal(err)
	}
	active := flattenTaskRow(ctx, t, db, receipt.Id)
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	key := domain.LeaseKey{
		Kind:       domain.LeaseKindStrategy,
		StrategyID: receipt.StrategyInstanceId,
	}
	if err := leases.Release(ctx, domain.LeaseToken{
		Key:     key,
		OwnerID: submit.Input.Authority.OwnerID,
		Epoch:   submit.Input.Authority.OwnerEpoch,
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := ownerid.New(ctx, "flatten-takeover")
	if err != nil {
		t.Fatal(err)
	}
	current, err := leases.Acquire(ctx, key, owner, 30*time.Second)
	if err != nil || current.Epoch <= submit.Input.Authority.OwnerEpoch {
		t.Fatal("takeover did not advance actual retained lease epoch", err)
	}
	authority := submit.Input.Authority
	authority.OwnerID, authority.OwnerEpoch = current.OwnerID, current.Epoch
	setFlattenRuntimeOwner(ctx, t, db, authority)
	assertDatabaseCode(t, updateFlattenTaskClaim(ctx, db, flattenTaskClaim(active, submit.Input.Authority)), "23514")
	if err := updateFlattenTaskClaim(ctx, db, flattenTaskClaim(active, authority)); err != nil {
		t.Fatal("current takeover could not retain the original active work", err)
	}
	taken := flattenTaskRow(ctx, t, db, receipt.Id)
	if taken.Id != queued.Id || taken.FlatteningId != receipt.Id || taken.State != string(domain.AdminFlattenActive) || taken.Revision != 3 ||
		taken.GetOwnerId() != current.OwnerID || taken.GetOwnerEpoch() != current.Epoch {
		t.Fatal("takeover invented a new task or lost exact ownership history")
	}
	assertPortfolioRowCount(ctx, t, db, &executionmodelv1.AdminAccountFlatteningTask{}, 1)
	assertPortfolioRowCount(ctx, t, db, &executionmodelv1.OrderIntent{}, 0)
	assertGatewayOrderCount(ctx, t, db, 0)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
}

func flattenTaskRow(ctx context.Context, t *testing.T, db *gorm.DB, flatteningID int64) *executionmodelv1.AdminAccountFlatteningTask {
	t.Helper()
	var row executionmodelv1.AdminAccountFlatteningTask
	if err := db.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlatteningTask{}).Where("flattening_id = ?", flatteningID).Take(&row).Error; err != nil {
		t.Fatal("missing durable flatten work", err)
	}
	return &row
}

func flattenTaskClaim(row *executionmodelv1.AdminAccountFlatteningTask, authority domain.StrategyAuthority) *executionmodelv1.AdminAccountFlatteningTask {
	claim := proto.CloneOf(row)
	claim.State = string(domain.AdminFlattenActive)
	claim.Revision++
	claim.OwnerId = proto.String(authority.OwnerID)
	claim.OwnerEpoch = proto.Int64(authority.OwnerEpoch)
	claim.ClaimedAt = nil
	return claim
}

func updateFlattenTaskClaim(ctx context.Context, db *gorm.DB, claim *executionmodelv1.AdminAccountFlatteningTask) error {
	if claim.ClaimedAt == nil {
		var clock struct{ NowMS int64 }
		if err := db.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlatteningTask{}).Where("id = ?", claim.Id).
			Select("floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint AS now_ms").Take(&clock).Error; err != nil {
			return err
		}
		claim.ClaimedAt = &clock.NowMS
	}
	// These persisted observations use PostgreSQL time. GORM's host-clock
	// auto timestamp must not introduce a different time domain into the proof.
	claim.UpdateAt = claim.GetClaimedAt()
	return db.WithContext(ctx).Model(&executionmodelv1.AdminAccountFlatteningTask{}).Where("id = ? AND revision = ?", claim.Id, claim.Revision-1).
		Select("State", "Revision", "OwnerId", "OwnerEpoch", "ClaimedAt", "UpdateAt").UpdateColumns(claim).Error
}

func setFlattenRuntimeOwner(ctx context.Context, t *testing.T, db *gorm.DB, authority domain.StrategyAuthority) {
	t.Helper()
	if err := db.WithContext(ctx).Model(&strategymodelv1.StrategyInstance{}).Where("id = ?", authority.StrategyID).
		Select("RuntimeOwnerId", "RuntimeOwnerEpoch", "State").Updates(&strategymodelv1.StrategyInstance{
		RuntimeOwnerId:    authority.OwnerID,
		RuntimeOwnerEpoch: authority.OwnerEpoch,
		State:             string(domain.StrategyStateRecovering),
	}).Error; err != nil {
		t.Fatal(err)
	}
}
