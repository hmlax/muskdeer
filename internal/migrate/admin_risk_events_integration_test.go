//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminRiskEventsRetainRawProducerHistoryAfterSourceRemoval(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, _, submit := adminCreationFixture(ctx, t, db)
	viewer := riskActorFixture(ctx, t, db, "risk-history-viewer", domain.AdminRoleViewer, 71, domain.AdminSessionTTL)
	evaluator, err := repository.NewRiskEvaluator(db)
	if err != nil {
		t.Fatal(err)
	}
	refreshRiskSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	if decision, err := evaluator.Evaluate(ctx, submit.Input.Authority.StrategyID); err != nil || !decision.Allowed {
		t.Fatal("actual risk producer did not reach ready", err)
	}
	core, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.Fill(ctx, riskEventFillRequest(submit, live.ClientOrderID)); err != nil {
		t.Fatal(err)
	}
	source := riskEventSourceRow(ctx, t, db, submit.Input.Authority.StrategyID)
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("id = ?", source.Id).Delete(&modelv1.RiskSnapshot{}).Error; err != nil {
		t.Fatal(err)
	}
	stored := riskEventRows(ctx, t, db, source.StrategyInstanceId)
	if len(stored) != 4 {
		t.Fatal("fixture failed to retain actual baseline, ready, fill and removal history")
	}
	store := adminRiskEventsStore(t, db)
	filter := domain.AdminRiskEventFilter{
		Page:       domain.AdminPage{Limit: 10},
		StrategyID: source.StrategyInstanceId,
	}
	page, err := store.List(ctx, viewer.Token, filter)
	if err != nil || len(page.Items) != 4 || page.NextAfterID != 0 {
		t.Fatal("source removal hid immutable history", err)
	}
	for i, value := range page.Items {
		row := stored[i]
		assertAdminRiskEventRecorded(t, value, row)
		detail, err := store.Detail(ctx, viewer.Token, value.ID)
		if err != nil || detail.ID != value.ID || detail.FeedID == nil || row.MarketFeedId == nil || *detail.FeedID != *row.MarketFeedId {
			t.Fatal("detail lost historical market identity", err)
		}
	}
	if page.Items[2].RecordedReconciled || !page.Items[2].RecordedAllowNewRisk || page.Items[2].Reason != domain.RiskReasonReady || !page.Items[3].SnapshotDeleted {
		t.Fatal("raw unreconciled/old-ready history became a fabricated current permit")
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.RiskSnapshot{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.RiskSnapshotEvent{}, 4)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorFill{}, 1)
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "799.8", "100.1")
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
}

func assertAdminRiskEventRecorded(t *testing.T, value domain.AdminRiskEvent, row *modelv1.RiskSnapshotEvent) {
	t.Helper()
	if value.ID != row.Id || value.SnapshotID != row.RiskSnapshotId || value.SnapshotRevision != row.RiskSnapshotRevision ||
		value.StrategyID != row.StrategyInstanceId || value.AccountID != row.AccountId || value.InstrumentID != row.InstrumentId ||
		value.Source != domain.MarketSource(row.MarketDataSource) || value.ConfigVersion != row.ConfigVersion ||
		value.Kind != domain.RiskSnapshotEventKind(row.Kind) || value.Reason != domain.RiskReason(row.ReasonCode) || value.SnapshotDeleted != row.SnapshotDeleted {
		t.Fatal("query changed original recorded identity")
	}
	if value.RecordedReconciled != row.Reconciled || value.RecordedAllowNewRisk != row.AllowNewRisk ||
		value.RecordedAt.UnixMilli() != row.CreateAt || value.ObservedAt.UnixMilli() != row.ObservedAt || value.ExpiresAt.UnixMilli() != row.ExpiresAt ||
		value.MarketSequence != row.MarketSequence || value.PublicationRevision != row.MarketRevision || value.SourceVersion != row.MarketSourceVersion ||
		value.PublisherEpoch != row.MarketEpoch || value.MarketMaximumAge.Milliseconds() != row.MarketMaximumAgeMs {
		t.Fatal("query changed original recorded observation")
	}
}

func TestAdminRiskEventsEmptyHistoryDoesNotInitializeRiskOrAccounts(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	actor := adminControlSessionFixtureForRole(ctx, t, db, domain.AdminRoleViewer)
	store := adminRiskEventsStore(t, db)
	for _, filter := range []domain.AdminRiskEventFilter{
		{Page: domain.AdminPage{Limit: 1}},
		{
			Page:       domain.AdminPage{Limit: 1},
			StrategyID: 9007199254740993,
		},
		{
			Page:         domain.AdminPage{Limit: 1},
			AccountID:    "unprovisioned-risk-account",
			InstrumentID: 9007199254740994,
		},
	} {
		page, err := store.List(ctx, actor.Token, filter)
		if err != nil || page.Items == nil || len(page.Items) != 0 || page.NextAfterID != 0 {
			t.Fatal("absent historical scope became data or an active-resource requirement", err)
		}
	}
	if _, err := store.Detail(ctx, actor.Token, 1); !errors.Is(err, domain.ErrAdminNotFound) {
		t.Fatal("absent immutable event became a fabricated baseline", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.RiskSnapshot{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.RiskSnapshotEvent{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorAccount{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorBalance{}, 0)
}

func adminRiskEventsStore(t *testing.T, db *gorm.DB) domain.AdminRiskEvents {
	t.Helper()
	store, err := repository.NewAdminRiskEvents(db)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
