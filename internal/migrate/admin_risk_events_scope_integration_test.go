//go:build integration

package migrate

import (
	"context"
	"slices"
	"testing"
	"time"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminRiskEventsPageAndFilterHistoricalScopesWithoutLiveAccounts(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	created, err := creator.Create(ctx, actor.Token, actor.CSRF, adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceOKX))
	if err != nil {
		t.Fatal(err)
	}
	evaluator, err := repository.NewRiskEvaluator(db)
	if err != nil {
		t.Fatal(err)
	}
	if decision, err := evaluator.Evaluate(ctx, created.StrategyID); err != nil || decision.Reason != domain.RiskReasonStopped || decision.Allowed {
		t.Fatal("stopped independent source fixture was not persisted", err)
	}
	unfunded := adminRiskEventUnfundedSource(ctx, t, db)
	original := riskEventSourceRow(ctx, t, db, submit.Input.Authority.StrategyID)
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("id = ?", original.Id).
		Select("AllowNewRisk", "ReasonCode", "Revision").Updates(&modelv1.RiskSnapshot{
		AllowNewRisk: false,
		ReasonCode:   string(domain.RiskReasonBudget),
		Revision:     original.Revision + 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	firstRows := riskEventRows(ctx, t, db, original.StrategyInstanceId)
	gridRows := riskEventRows(ctx, t, db, created.StrategyID)
	otherRows := riskEventRows(ctx, t, db, unfunded.StrategyInstanceId)
	store := adminRiskEventsStore(t, db)
	filter := domain.AdminRiskEventFilter{Page: domain.AdminPage{Limit: 1}}
	var ids []int64
	for range 4 {
		page, err := store.List(ctx, actor.Token, filter)
		if err != nil || len(page.Items) != 1 {
			t.Fatal("history cursor lost a bounded page", err)
		}
		ids = append(ids, page.Items[0].ID)
		filter.Page.AfterID = page.NextAfterID
	}
	if filter.Page.AfterID != 0 || !slices.Equal(ids, []int64{firstRows[0].Id, gridRows[0].Id, otherRows[0].Id, firstRows[1].Id}) {
		t.Fatal("event cursor was replaced by source revision or strategy identity")
	}
	assertAdminRiskEventFilters(ctx, t, store, actor.Token, original, unfunded, created.StrategyID, ids)
	value, err := store.Detail(ctx, actor.Token, otherRows[0].Id)
	if err != nil || value.FeedID != nil || value.MarketSequence != 9007199254740993 || value.RecordedReconciled || !value.SnapshotDeleted {
		t.Fatal("retired/unprovisioned historical scope was lost or precision changed", err)
	}
	var accounts int64
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorAccount{}).Where("account_id = ?", unfunded.AccountId).Count(&accounts).Error; err != nil || accounts != 0 {
		t.Fatal("history query initialized a missing core account", err)
	}
}

func assertAdminRiskEventFilters(ctx context.Context, t *testing.T, store domain.AdminRiskEvents, token string, original, other *modelv1.RiskSnapshot, gridID int64, ids []int64) {
	t.Helper()
	for _, test := range []struct {
		filter domain.AdminRiskEventFilter
		ids    []int64
	}{
		{
			filter: domain.AdminRiskEventFilter{StrategyID: original.StrategyInstanceId},
			ids:    []int64{ids[0], ids[3]},
		},
		{
			filter: domain.AdminRiskEventFilter{AccountID: original.AccountId},
			ids:    []int64{ids[0], ids[1], ids[3]},
		},
		{
			filter: domain.AdminRiskEventFilter{InstrumentID: original.InstrumentId},
			ids:    []int64{ids[0], ids[1], ids[3]},
		},
		{
			filter: domain.AdminRiskEventFilter{Source: domain.MarketSourceOKX},
			ids:    []int64{ids[1]},
		},
		{
			filter: domain.AdminRiskEventFilter{Kind: domain.RiskSnapshotEventChanged},
			ids:    []int64{ids[3]},
		},
		{
			filter: domain.AdminRiskEventFilter{Reason: domain.RiskReasonBudget},
			ids:    []int64{ids[3]},
		},
		{
			filter: domain.AdminRiskEventFilter{
				StrategyID:   gridID,
				AccountID:    original.AccountId,
				InstrumentID: original.InstrumentId,
				Source:       domain.MarketSourceOKX,
				Kind:         domain.RiskSnapshotEventBaseline,
				Reason:       domain.RiskReasonStopped,
			},
			ids: []int64{ids[1]},
		},
		{
			filter: domain.AdminRiskEventFilter{AccountID: other.AccountId},
			ids:    []int64{ids[2]},
		},
		{
			filter: domain.AdminRiskEventFilter{
				StrategyID: original.StrategyInstanceId,
				Source:     domain.MarketSourceOKX,
			},
			ids: []int64{},
		},
	} {
		test.filter.Page = domain.AdminPage{Limit: 10}
		page, err := store.List(ctx, token, test.filter)
		var actual []int64
		for _, value := range page.Items {
			actual = append(actual, value.ID)
		}
		if err != nil || page.Items == nil || page.NextAfterID != 0 || !slices.Equal(actual, test.ids) {
			t.Fatal("exact historical filters mixed scopes or required current resources", err)
		}
	}
}

// Explicit retired history fixture has no core account and no live market feed.
func adminRiskEventUnfundedSource(ctx context.Context, t *testing.T, db *gorm.DB) *modelv1.RiskSnapshot {
	t.Helper()
	instrument := identityInstrument()
	instrument.Symbol, instrument.BaseAsset = "ETH-USDT", "ETH"
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Create(instrument).Error; err != nil {
		t.Fatal(err)
	}
	instance := identityInstance(instrument.Id, "unfunded-retired-risk-history", domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance)
	instance.AccountId = "unprovisioned-risk-history"
	retired := time.Now().UnixMilli()
	instance.DeleteAt = &retired
	config := storedParameterFixture(domain.StrategyTemplateMarketMaker)
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Create(instance).Error; err != nil {
			return err
		}
		config.InstanceId = instance.Id
		return tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Create(config).Error
	}); err != nil {
		t.Fatal(err)
	}
	row := &modelv1.RiskSnapshot{
		StrategyInstanceId: instance.Id,
		AccountId:          instance.AccountId,
		InstrumentId:       instrument.Id,
		ConfigVersion:      1,
		MarketDataSource:   string(domain.MarketSourceBinance),
		MarketSequence:     9007199254740993,
		MarketMaximumAgeMs: 5000,
		ObservedAt:         1000,
		ExpiresAt:          2000,
		ReasonCode:         string(domain.RiskReasonUnreconciled),
		Revision:           1,
		DeleteAt:           &retired,
	}
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Create(row).Error; err != nil {
		t.Fatal(err)
	}
	return row
}
