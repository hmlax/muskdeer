package repository

import (
	"context"
	"errors"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func riskBudgetsEnabled(ctx context.Context, tx *gorm.DB, strategy *modelv1.StrategyInstance, quoteAsset string) (bool, error) {
	var account modelv1.AccountBudget
	err := tx.WithContext(ctx).Model(&modelv1.AccountBudget{}).Where("account_id = ? AND delete_at IS NULL", strategy.AccountId).Clauses(clause.Locking{Strength: "SHARE"}).First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var budget modelv1.StrategyBudget
	err = tx.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Where("strategy_instance_id = ? AND account_id = ? AND delete_at IS NULL", strategy.Id, strategy.AccountId).Clauses(clause.Locking{Strength: "SHARE"}).First(&budget).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !account.Enabled || !budget.Enabled || account.Asset != quoteAsset {
		return false, nil
	}
	if _, err := reserveWithinLimit(account.ReservedNotional, account.LimitNotional, decimal.Zero); err != nil {
		return false, err
	}
	if _, err := reserveWithinLimit(budget.ReservedNotional, budget.LimitNotional, decimal.Zero); err != nil {
		return false, err
	}
	global, err := globalRiskBudgets(ctx, tx, strategy.InstrumentId, quoteAsset, "SHARE")
	if errors.Is(err, domain.ErrRiskDenied) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, row := range global {
		if !row.Enabled {
			return false, nil
		}
	}
	return true, nil
}

func riskPortfolioReconciled(ctx context.Context, tx *gorm.DB, strategy *modelv1.StrategyInstance, core coreRows) (bool, error) {
	var row modelv1.PortfolioAccount
	err := tx.WithContext(ctx).Model(&modelv1.PortfolioAccount{}).Where("account_id = ? AND delete_at IS NULL", strategy.AccountId).Clauses(clause.Locking{Strength: "SHARE"}).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !row.Reconciled || row.InstrumentId != strategy.InstrumentId || row.BaseAsset != core.instrument.BaseAsset || row.QuoteAsset != core.instrument.QuoteAsset {
		return false, nil
	}
	count, latest, err := portfolioSourceStats(ctx, tx, strategy.AccountId, strategy.InstrumentId)
	if err != nil {
		return false, err
	}
	var projected int64
	if err := tx.WithContext(ctx).Model(&modelv1.PortfolioFill{}).Where("account_id = ? AND instrument_id = ? AND delete_at IS NULL", strategy.AccountId, strategy.InstrumentId).Count(&projected).Error; err != nil {
		return false, err
	}
	if latest != row.LastSourceSequence || count != projected || count > domain.MaxPortfolioFacts {
		return false, nil
	}
	snapshot, err := portfolioSnapshot(&row)
	if err != nil {
		return false, err
	}
	if err := comparePortfolioCore(ctx, tx, core, snapshot.Values); err != nil {
		if errors.Is(err, domain.ErrReconciliationMismatch) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func saveRiskDecision(ctx context.Context, tx *gorm.DB, strategy *modelv1.StrategyInstance, decision domain.RiskDecision, maximumAgeMS int64) error {
	version, source := strategy.RequestedConfigVersion, strategy.MarketDataSource
	if strategy.ActiveConfigVersion != nil && strategy.ActiveMarketDataSource != nil {
		version, source = *strategy.ActiveConfigVersion, *strategy.ActiveMarketDataSource
	}
	row := &modelv1.RiskSnapshot{
		StrategyInstanceId: strategy.Id,
		AccountId:          strategy.AccountId,
		InstrumentId:       strategy.InstrumentId,
		ConfigVersion:      version,
		MarketDataSource:   source,
		ObservedAt:         decision.ObservedAt.UnixMilli(),
		ExpiresAt:          decision.ExpiresAt.UnixMilli(),
		Reconciled:         decision.Reconciled,
		AllowNewRisk:       decision.Allowed,
		MarketMaximumAgeMs: maximumAgeMS,
		ReasonCode:         string(decision.Reason),
		Revision:           1,
	}
	if decision.Market.FeedID > 0 {
		id := decision.Market.FeedID
		row.MarketFeedId = &id
		row.MarketRevision = decision.Market.Revision
		row.MarketSourceVersion = decision.Market.Reading.SourceVersion
		row.MarketEpoch = decision.Market.OwnerEpoch
		row.MarketSequence = decision.Market.Reading.Sequence
	}
	var previous modelv1.RiskSnapshot
	err := tx.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ?", strategy.Id).Clauses(clause.Locking{Strength: "UPDATE"}).First(&previous).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return tx.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Create(row).Error
	}
	if err != nil {
		return err
	}
	// A retired grant is never silently resurrected by a periodic evaluator.
	if previous.DeleteAt != nil {
		return domain.ErrRiskDenied
	}
	row.Revision = previous.Revision + 1
	result := tx.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("id = ? AND revision = ?", previous.Id, previous.Revision).
		Select("AccountId", "InstrumentId", "ConfigVersion", "MarketDataSource", "MarketEpoch", "MarketSequence", "ObservedAt", "ExpiresAt", "Reconciled", "AllowNewRisk", "Revision", "MarketFeedId", "MarketRevision", "MarketSourceVersion", "MarketMaximumAgeMs", "ReasonCode").Updates(row)
	return reservationResult(result)
}
