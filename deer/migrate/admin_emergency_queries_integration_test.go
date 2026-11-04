//go:build integration

package migrate

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	strategymodelv1 "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/repository"
)

func TestAdminEmergencyQueriesObserveStoredProofWithoutAdvancingEmptyScope(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	viewer := riskActorFixture(ctx, t, db, "emergency-query-viewer", domain.AdminRoleViewer, 191, domain.AdminSessionTTL)
	store := emergencyAcceptanceStore(t, db)
	accepted, err := store.Request(ctx, actor.Token, actor.CSRF, emergencyAcceptanceRequest(submit.Input.Authority.StrategyID))
	if err != nil {
		t.Fatal(err)
	}
	before := emergencyQueryTask(ctx, t, db, accepted.ID)
	observed, err := store.Observation(ctx, viewer.Token, accepted.StrategyID, accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertEmergencyUnenrolledObservation(t, observed, accepted)
	if !proto.Equal(before, emergencyQueryTask(ctx, t, db, accepted.ID)) {
		t.Fatal("management read advanced the durable worker task")
	}
	if _, err := emergencyProgressStore(t, db).Prepare(ctx, accepted.StrategyID, 0); err != nil {
		t.Fatal(err)
	}
	completed, err := store.Observation(ctx, viewer.Token, accepted.StrategyID, accepted.ID)
	if err != nil || completed.Record.Acceptance != accepted || completed.Record.State != domain.AdminEmergencyCancelSucceeded || completed.Record.CompletedAt.IsZero() ||
		!completed.Record.EnrollmentComplete || !completed.Progress.EnrollmentComplete || completed.Progress.Targets != 0 || completed.Record.Revision <= observed.Record.Revision {
		t.Fatal("query lost explicitly persisted empty completion", err)
	}
	if result, err := store.Observation(ctx, viewer.Token, accepted.StrategyID+1, accepted.ID); !errors.Is(err, domain.ErrAdminNotFound) || result.Record.Acceptance.ID != 0 || !result.ObservedAt.IsZero() {
		t.Fatal("query leaked an acceptance across its declared strategy", err)
	}
	if result, err := store.Observation(ctx, viewer.Token, 0, accepted.ID); !errors.Is(err, domain.ErrInvalidAdminQuery) || result.Record.Acceptance.ID != 0 {
		t.Fatal("invalid observation scope reached storage", err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if result, err := store.Observation(canceled, viewer.Token, accepted.StrategyID, accepted.ID); !errors.Is(err, context.Canceled) || result.Record.Acceptance.ID != 0 {
		t.Fatal("observation ignored parent cancellation", err)
	}
	if result, err := store.History(canceled, viewer.Token, accepted.StrategyID, domain.AdminPage{Limit: 1}); !errors.Is(err, context.Canceled) || result.Items != nil {
		t.Fatal("history ignored parent cancellation", err)
	}
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx, viewer.Token, viewer.CSRF); err != nil {
		t.Fatal(err)
	}
	if result, err := store.Observation(ctx, viewer.Token, accepted.StrategyID, accepted.ID); !errors.Is(err, domain.ErrUnauthenticated) || result.Record.Acceptance.ID != 0 {
		t.Fatal("revoked viewer retained scoped progress", err)
	}
	assertPortfolioRowCount(ctx, t, db, &executionmodelv1.AdminOrderCancellation{}, 0)
	assertPortfolioRowCount(ctx, t, db, &strategymodelv1.StrategyAction{}, 0)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertGatewayOrderCount(ctx, t, db, 0)
}

func assertEmergencyUnenrolledObservation(t *testing.T, observed domain.AdminEmergencyCancelObservation, accepted domain.AdminEmergencyCancelAcceptance) {
	t.Helper()
	if observed.Record.Acceptance != accepted || observed.Record.State != domain.AdminEmergencyCancelAccepted || observed.Record.Revision != 1 ||
		observed.Record.EnrollmentComplete || !observed.Record.CompletedAt.IsZero() || observed.Progress.Targets != 0 || observed.Progress.EnrollmentComplete || observed.ObservedAt.UnixMilli() <= 0 {
		t.Fatal("query inferred success from an empty, unenrolled scope")
	}
}

func TestAdminEmergencyQueriesPageOnlyDeclaredHistoryWithBoundedCursor(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	sibling, err := creator.Create(ctx, actor.Token, actor.CSRF, adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceOKX))
	if err != nil {
		t.Fatal(err)
	}
	store := emergencyAcceptanceStore(t, db)
	ids := make([]int64, 0, 3)
	for index := range 3 {
		request := emergencyAcceptanceRequest(submit.Input.Authority.StrategyID)
		request.Key = "emergency-query-page-" + strconv.Itoa(index)
		request.ExpectedRiskRevision = int64(index + 1)
		receipt, err := store.Request(ctx, actor.Token, actor.CSRF, request)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, receipt.ID)
	}
	siblingRequest := emergencyAcceptanceRequest(sibling.StrategyID)
	siblingRequest.Key = "emergency-query-sibling-0001"
	if _, err := store.Request(ctx, actor.Token, actor.CSRF, siblingRequest); err != nil {
		t.Fatal(err)
	}
	first, err := store.History(ctx, actor.Token, submit.Input.Authority.StrategyID, domain.AdminPage{Limit: 1})
	if err != nil || len(first.Items) != 1 || first.Items[0].Acceptance.ID != ids[0] || first.Items[0].State != domain.AdminEmergencyCancelAccepted || first.NextAfterID != ids[0] || first.ObservedAt.UnixMilli() <= 0 {
		t.Fatal("scope history lost its bounded initial page", err)
	}
	last, err := store.History(ctx, actor.Token, submit.Input.Authority.StrategyID, domain.AdminPage{
		AfterID: first.NextAfterID,
		Limit:   2,
	})
	if err != nil || len(last.Items) != 2 || last.Items[0].Acceptance.ID != ids[1] || last.Items[1].Acceptance.ID != ids[2] || last.NextAfterID != 0 {
		t.Fatal("scope history crossed a sibling or lost final cursor", err)
	}
	empty, err := store.History(ctx, actor.Token, submit.Input.Authority.StrategyID, domain.AdminPage{
		AfterID: ids[2],
		Limit:   1,
	})
	if err != nil || empty.Items == nil || len(empty.Items) != 0 || empty.NextAfterID != 0 || empty.ObservedAt.UnixMilli() <= 0 {
		t.Fatal("exhausted history became missing or invented sibling work", err)
	}
	for _, page := range []domain.AdminPage{
		{Limit: 0},
		{Limit: domain.MaxAdminPageSize + 1},
		{
			AfterID: -1,
			Limit:   1,
		},
	} {
		if result, err := store.History(ctx, actor.Token, submit.Input.Authority.StrategyID, page); !errors.Is(err, domain.ErrInvalidAdminQuery) || result.Items != nil || !result.ObservedAt.IsZero() {
			t.Fatal("invalid history bounds leaked a partial page", err)
		}
	}
	assertPortfolioRowCount(ctx, t, db, &executionmodelv1.AdminEmergencyCancellationTask{}, 4)
	assertPortfolioRowCount(ctx, t, db, &executionmodelv1.AdminOrderCancellation{}, 0)
	assertGatewayOrderCount(ctx, t, db, 0)
}

func TestAdminEmergencyQueriesAuthorityTaskAndCountsShareRepeatableSnapshot(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	_, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	if _, err := intents.Accept(ctx, submit.Input, submit.ExecutionAuthority); err != nil {
		t.Fatal(err)
	}
	store := emergencyAcceptanceStore(t, db)
	accepted, err := store.Request(ctx, actor.Token, actor.CSRF, emergencyAcceptanceRequest(submit.Input.Authority.StrategyID))
	if err != nil {
		t.Fatal(err)
	}
	viewer := riskActorFixture(ctx, t, db, "emergency-snapshot-viewer", domain.AdminRoleViewer, 201, domain.AdminSessionTTL)
	work := emergencyProgressStore(t, db)
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	const callback = "test:emergency-task-snapshot"
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(query *gorm.DB) {
		if _, ok := query.Statement.Model.(*executionmodelv1.AdminEmergencyCancellationTask); !ok || changed || query.Error != nil {
			return
		}
		changed = true
		if _, err := work.Prepare(ctx, accepted.StrategyID, 0); err != nil {
			_ = query.AddError(err)
			return
		}
		_ = query.AddError(auth.Logout(ctx, viewer.Token, viewer.CSRF))
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Query().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	first, err := store.Observation(ctx, viewer.Token, accepted.StrategyID, accepted.ID)
	if err != nil || !changed || first.Record.EnrollmentComplete || first.Progress.EnrollmentComplete || first.Progress.Targets != 0 {
		t.Fatal("observation mixed newer enrollment with its authorized task snapshot", err)
	}
	second, err := store.Observation(ctx, actor.Token, accepted.StrategyID, accepted.ID)
	if err != nil || !second.Record.EnrollmentComplete || !second.Progress.EnrollmentComplete || second.Progress.Targets != 1 || second.Record.State != domain.AdminEmergencyCancelAccepted {
		t.Fatal("next authorized snapshot did not expose committed enrollment", err)
	}
	if result, err := store.Observation(ctx, viewer.Token, accepted.StrategyID, accepted.ID); !errors.Is(err, domain.ErrUnauthenticated) || result.Record.Acceptance.ID != 0 {
		t.Fatal("following observation reused revoked snapshot authority", err)
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertGatewayOrderCount(ctx, t, db, 0)
}

func emergencyQueryTask(ctx context.Context, t *testing.T, db *gorm.DB, id int64) *executionmodelv1.AdminEmergencyCancellationTask {
	t.Helper()
	var task executionmodelv1.AdminEmergencyCancellationTask
	if err := db.WithContext(ctx).Model(&executionmodelv1.AdminEmergencyCancellationTask{}).Where("emergency_cancellation_id = ?", id).Take(&task).Error; err != nil {
		t.Fatal(err)
	}
	return &task
}

func TestAdminEmergencyQueriesHistoryKeepsAcceptanceAndTaskInSameSnapshot(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	store := emergencyAcceptanceStore(t, db)
	accepted, err := store.Request(ctx, actor.Token, actor.CSRF, emergencyAcceptanceRequest(submit.Input.Authority.StrategyID))
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	const callback = "test:emergency-history-snapshot"
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(query *gorm.DB) {
		if _, ok := query.Statement.Dest.(*[]*executionmodelv1.AdminEmergencyCancellation); !ok || changed || query.Error != nil {
			return
		}
		changed = true
		if _, err := emergencyProgressStore(t, db).Prepare(ctx, accepted.StrategyID, 0); err != nil {
			_ = query.AddError(err)
			return
		}
		next := emergencyAcceptanceRequest(accepted.StrategyID)
		next.Key, next.ExpectedRiskRevision = "emergency-concurrent-history-0001", 2
		_, err := store.Request(ctx, actor.Token, actor.CSRF, next)
		_ = query.AddError(err)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Query().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	first, err := store.History(ctx, actor.Token, accepted.StrategyID, domain.AdminPage{Limit: 2})
	if err != nil || !changed || len(first.Items) != 1 || first.Items[0].Acceptance != accepted || first.Items[0].State != domain.AdminEmergencyCancelAccepted || first.Items[0].EnrollmentComplete || first.NextAfterID != 0 {
		t.Fatal("history mixed a newer task or acceptance into its original snapshot", err)
	}
	next, err := store.History(ctx, actor.Token, accepted.StrategyID, domain.AdminPage{Limit: 2})
	if err != nil || len(next.Items) != 2 || next.Items[0].State != domain.AdminEmergencyCancelSucceeded || next.Items[1].State != domain.AdminEmergencyCancelAccepted {
		t.Fatal("next history did not expose separately committed durable changes", err)
	}
}

func TestAdminEmergencyQueriesUseDatabaseReadOnlyAndReturnNoPartialResults(t *testing.T) {
	for _, history := range []bool{false, true} {
		t.Run(strconv.FormatBool(history), func(t *testing.T) {
			db, _ := migrationDatabase(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			actor, _, submit := adminCreationFixture(ctx, t, db)
			store := emergencyAcceptanceStore(t, db)
			accepted, err := store.Request(ctx, actor.Token, actor.CSRF, emergencyAcceptanceRequest(submit.Input.Authority.StrategyID))
			if err != nil {
				t.Fatal(err)
			}
			before := emergencyQueryTask(ctx, t, db, accepted.ID)
			attempted := false
			var writeErr error
			const callback = "test:emergency-read-only"
			if err := db.Callback().Query().After("gorm:query").Register(callback, func(query *gorm.DB) {
				if _, ok := query.Statement.Model.(*executionmodelv1.AdminEmergencyCancellationTask); !ok || attempted || query.Error != nil {
					return
				}
				attempted = true
				writeErr = query.WithContext(ctx).Session(&gorm.Session{NewDB: true}).Model(&executionmodelv1.SimulatorAccount{}).
					Where("account_id = ?", submit.Input.AccountID).Update("enabled", false).Error
				_ = query.AddError(writeErr)
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Callback().Query().Remove(callback); err != nil {
					t.Error(err)
				}
			})
			assertEmergencyFailedQuery(ctx, t, store, actor.Token, accepted, history)
			if !attempted {
				t.Fatal("read-only test did not reach the actual task query")
			}
			assertDatabaseCode(t, writeErr, "25006")
			var account executionmodelv1.SimulatorAccount
			if err := db.WithContext(ctx).Model(&executionmodelv1.SimulatorAccount{}).Where("account_id = ?", submit.Input.AccountID).Take(&account).Error; err != nil || !account.Enabled ||
				!proto.Equal(before, emergencyQueryTask(ctx, t, db, accepted.ID)) {
				t.Fatal("read failure changed funds or task progress", err)
			}
		})
	}
}

func assertEmergencyFailedQuery(ctx context.Context, t *testing.T, store domain.AdminEmergencyReader, token string, accepted domain.AdminEmergencyCancelAcceptance, history bool) {
	t.Helper()
	if history {
		page, err := store.History(ctx, token, accepted.StrategyID, domain.AdminPage{Limit: 1})
		if !errors.Is(err, domain.ErrAdminUnavailable) || page.Items != nil || !page.ObservedAt.IsZero() || page.NextAfterID != 0 {
			t.Fatal("history failure leaked partial accepted records or time", err)
		}
		return
	}
	value, err := store.Observation(ctx, token, accepted.StrategyID, accepted.ID)
	if !errors.Is(err, domain.ErrAdminUnavailable) || value.Record.Acceptance.ID != 0 || !value.ObservedAt.IsZero() || value.Progress.Targets != 0 {
		t.Fatal("observation failure leaked partial task, counts or time", err)
	}
}

func assertEmergencyHistoricalFailureAfterFill(ctx context.Context, t *testing.T, db *gorm.DB, actor domain.AdminLogin, accepted domain.AdminEmergencyCancelAcceptance, order domain.IntentSnapshot, core domain.Simulator, intents domain.IntentExecutionRepository) {
	t.Helper()
	reader := emergencyAcceptanceStore(t, db)
	before, err := reader.Observation(ctx, actor.Token, accepted.StrategyID, accepted.ID)
	if err != nil || before.Record.State != domain.AdminEmergencyCancelFailed || before.Progress.Failed != 1 || before.Progress.Terminal != 0 {
		t.Fatal("failure observation lost still-live obligation", err)
	}
	if _, err := core.Fill(ctx, domain.CoreFillRequest{
		AccountID:           order.Input.AccountID,
		ClientOrderID:       order.ClientOrderID,
		TradeID:             "emergency-historical-failure-later-fill",
		CounterpartySubject: "external-liquidity",
		Price:               order.Input.Price,
		Quantity:            order.Input.Quantity,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := intentServiceFixture(t, intents, core).Recover(ctx, order.Input.AccountID, order.ID); err != nil {
		t.Fatal(err)
	}
	after, err := reader.Observation(ctx, actor.Token, accepted.StrategyID, accepted.ID)
	if err != nil || after.Record != before.Record || after.Progress.Failed != 0 || after.Progress.Terminal != 1 || after.Progress.Targets != 1 {
		t.Fatal("current terminal counters rewrote immutable historical failure", err)
	}
	assertReservedBudget(ctx, t, db, order.Input.AccountID, "0")
}
