//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/ownerid"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminOverviewEmptyDatabaseDoesNotCreateStrategiesOrOwnership(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	actor := adminControlSessionFixtureForRole(ctx, t, db, domain.AdminRoleViewer)
	store, err := repository.NewAdminOverview(db)
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.Strategies(ctx, actor.Token, domain.AdminOverviewFilter{Page: domain.AdminPage{Limit: 1}})
	if err != nil || page.ObservedAt.UnixMilli() <= 0 || page.Total != 0 || page.RequestedRunning != 0 || page.StoredStates == nil || len(page.StoredStates) != 0 || page.Items == nil || len(page.Items) != 0 || page.NextAfterID != 0 {
		t.Fatal("empty overview fabricated metadata, health or ownership", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyInstance{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyLease{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.RiskSnapshot{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorBalance{}, 0)
}

func TestAdminOverviewCountsIgnoreCursorAndLeaseFactsNeverClaimProcessHealth(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	created, err := creator.Create(ctx, actor.Token, actor.CSRF, adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceOKX))
	if err != nil {
		t.Fatal(err)
	}
	adminRiskEventUnfundedSource(ctx, t, db) // Archived metadata is outside this active scope.
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	key := domain.LeaseKey{
		Kind:       domain.LeaseKindStrategy,
		StrategyID: submit.Input.Authority.StrategyID,
	}
	current, err := leases.Acquire(ctx, key, submit.Input.Authority.OwnerID, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	current, err = leases.Renew(ctx, current, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	adminOverviewRecoverRunning(ctx, t, db, submit, current)
	before := publicationInstance(ctx, t, db, key.StrategyID)
	riskBefore := riskEventSourceRow(ctx, t, db, key.StrategyID)
	store, err := repository.NewAdminOverview(db)
	if err != nil {
		t.Fatal(err)
	}
	filter := domain.AdminOverviewFilter{Page: domain.AdminPage{Limit: 1}}
	first, err := store.Strategies(ctx, actor.Token, filter)
	if err != nil || first.Total != 2 || first.RequestedRunning != 1 || len(first.StoredStates) != 2 || len(first.Items) != 1 || first.NextAfterID != key.StrategyID {
		t.Fatal("overview lost stored scope counts or bounded first page", err)
	}
	lease := first.Items[0].Lease
	if first.Items[0].Strategy.Spec.State != domain.StrategyStateRunning || lease == nil || !lease.OwnerRecorded || !lease.UnexpiredAtObservation || !lease.RuntimeMatchesLease || lease.Epoch != current.Epoch || !lease.ExpiresAt.Equal(current.ExpiresAt) {
		t.Fatal("current matching lease facts were not sampled independently")
	}
	assertAdminOverviewPages(ctx, t, store, actor.Token, filter, first.NextAfterID, created.StrategyID, submit.Input.AccountID)
	assertAdminOverviewZeroSlot(ctx, t, db, store, actor.Token, first.NextAfterID, created.StrategyID)
	assertAdminOverviewLeaseLifecycle(ctx, t, db, store, actor.Token, leases, key, current)
	if !proto.Equal(before, publicationInstance(ctx, t, db, key.StrategyID)) || !proto.Equal(riskBefore, riskEventSourceRow(ctx, t, db, key.StrategyID)) {
		t.Fatal("overview claimed or advanced the stored runtime")
	}
	assertGatewayOrderCount(ctx, t, db, 0)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
}

func assertAdminOverviewZeroSlot(ctx context.Context, t *testing.T, db *gorm.DB, store domain.AdminOverview, token string, after, id int64) {
	t.Helper()
	if err := db.WithContext(ctx).Model(&modelv1.StrategyLease{}).Create(&modelv1.StrategyLease{StrategyInstanceId: id}).Error; err != nil {
		t.Fatal(err)
	}
	value, err := store.Strategies(ctx, token, domain.AdminOverviewFilter{
		Page: domain.AdminPage{
			AfterID: after,
			Limit:   1,
		},
	})
	if err != nil || len(value.Items) != 1 || value.Items[0].Lease == nil || value.Items[0].Lease.Epoch != 0 || value.Items[0].Lease.OwnerRecorded || value.Items[0].Lease.UnexpiredAtObservation || value.Items[0].Lease.RuntimeMatchesLease {
		t.Fatal("real unclaimed database slot became missing or current ownership", err)
	}
}

func assertAdminOverviewPages(ctx context.Context, t *testing.T, store domain.AdminOverview, token string, filter domain.AdminOverviewFilter, after, gridID int64, account string) {
	t.Helper()
	filter.Page.AfterID = after
	last, err := store.Strategies(ctx, token, filter)
	if err != nil || last.Total != 2 || last.RequestedRunning != 1 || len(last.Items) != 1 || last.Items[0].Strategy.Spec.ID != gridID || last.Items[0].Lease != nil || last.NextAfterID != 0 {
		t.Fatal("cursor narrowed totals or invented an unclaimed lease", err)
	}
	filter.Page.AfterID = 9223372036854775807
	empty, err := store.Strategies(ctx, token, filter)
	if err != nil || empty.Total != 2 || len(empty.Items) != 0 || empty.NextAfterID != 0 {
		t.Fatal("empty page was mistaken for an empty strategy scope", err)
	}
	filter.Page.AfterID, filter.AccountID, filter.Source = 0, account, domain.MarketSourceOKX
	only, err := store.Strategies(ctx, token, filter)
	if err != nil || only.Total != 1 || only.RequestedRunning != 0 || len(only.Items) != 1 || only.Items[0].Strategy.Spec.ID != gridID {
		t.Fatal("requested source/account filters crossed their stored scope", err)
	}
	filter.AccountID = "missing-overview-account"
	absent, err := store.Strategies(ctx, token, filter)
	if err != nil || absent.Total != 0 || len(absent.Items) != 0 {
		t.Fatal("unknown overview scope fabricated current resources", err)
	}
}

func assertAdminOverviewLeaseLifecycle(ctx context.Context, t *testing.T, db *gorm.DB, store domain.AdminOverview, token string, leases domain.LeaseRepository, key domain.LeaseKey, current domain.LeaseToken) {
	t.Helper()
	filter := domain.AdminOverviewFilter{Page: domain.AdminPage{Limit: 1}}
	expireLeaseFixture(ctx, t, db, key)
	expired, err := store.Strategies(ctx, token, filter)
	if err != nil || expired.Items[0].Lease == nil || expired.Items[0].Lease.UnexpiredAtObservation || !expired.Items[0].Lease.RuntimeMatchesLease || expired.Items[0].Strategy.Spec.State != domain.StrategyStateRunning {
		t.Fatal("expired lease falsely changed or certified stored RUNNING", err)
	}
	replacement, err := ownerid.New(ctx, "overview-replacement")
	if err != nil {
		t.Fatal(err)
	}
	newOwner, err := leases.Acquire(ctx, key, replacement, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	taken, err := store.Strategies(ctx, token, filter)
	if err != nil || taken.Items[0].Lease == nil || taken.Items[0].Lease.Epoch != newOwner.Epoch || !taken.Items[0].Lease.UnexpiredAtObservation || taken.Items[0].Lease.RuntimeMatchesLease || taken.Items[0].Strategy.Spec.RuntimeOwnerEpoch != current.Epoch {
		t.Fatal("new lease silently adopted the previous runtime observation", err)
	}
	if err := leases.Release(ctx, newOwner); err != nil {
		t.Fatal(err)
	}
	released, err := store.Strategies(ctx, token, filter)
	if err != nil || released.Items[0].Lease == nil || !released.Items[0].Lease.OwnerRecorded || released.Items[0].Lease.ExpiresAt.UnixMilli() != 0 || released.Items[0].Lease.UnexpiredAtObservation || released.Items[0].Lease.RuntimeMatchesLease || released.Items[0].Lease.Epoch != newOwner.Epoch {
		t.Fatal("retained released epoch became a current owner", err)
	}

}

func adminOverviewRecoverRunning(ctx context.Context, t *testing.T, db *gorm.DB, submit domain.GatewaySubmit, token domain.LeaseToken) {
	t.Helper()
	lifecycle, err := repository.NewStrategyLifecycle(db)
	if err != nil {
		t.Fatal(err)
	}
	current, err := lifecycle.Instance(ctx, token.Key.StrategyID)
	if err != nil {
		t.Fatal(err)
	}
	recovering, err := lifecycle.Transition(ctx, current, token, domain.StrategyTransition{State: domain.StrategyStateRecovering})
	if err != nil {
		t.Fatal(err)
	}
	refreshRiskSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	if _, err := lifecycle.Transition(ctx, recovering, token, domain.StrategyTransition{
		State:             domain.StrategyStateRunning,
		ActivateRequested: true,
	}); err != nil {
		t.Fatal(err)
	}
}
