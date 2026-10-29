package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategyconfig"
)

type riskEvaluator struct{ db *gorm.DB }

// NewRiskEvaluator borrows an L1 database without granting or starting work.
func NewRiskEvaluator(db *gorm.DB) (domain.RiskEvaluator, error) {
	if db == nil {
		return nil, domain.ErrInvalidOrder
	}
	return &riskEvaluator{db: db}, nil
}

func (r *riskEvaluator) Evaluate(ctx context.Context, strategyID int64) (domain.RiskDecision, error) {
	if err := coreContext(ctx); err != nil {
		return domain.RiskDecision{}, err
	}
	if strategyID <= 0 {
		return domain.RiskDecision{}, domain.ErrInvalidOrder
	}
	var result domain.RiskDecision
	err := r.db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Transaction(func(tx *gorm.DB) error {
		var strategy modelv1.StrategyInstance
		// Serializes evaluations and configuration/state changes. Core holders
		// take this strategy lock before the instrument, preventing reverse waits.
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ? AND delete_at IS NULL", strategyID).Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).First(&strategy).Error; err != nil {
			return err
		}
		result = domain.RiskDecision{
			StrategyID: strategyID,
			Reason:     domain.RiskReasonStopped,
		}
		maximumAgeMS := int64(5000)
		if strategy.State == string(domain.StrategyStateRunning) && strategy.DesiredRunning && strategy.ActiveConfigVersion != nil && strategy.ActiveMarketDataSource != nil {
			var err error
			result, maximumAgeMS, err = evaluateRiskInputs(ctx, tx, &strategy)
			if err != nil {
				return err
			}
		}
		now, err := riskEvaluationTime(ctx, tx, strategyID)
		if err != nil {
			return err
		}
		result.ObservedAt = now
		result.ExpiresAt = now.Add(time.Millisecond)
		if result.Reason == domain.RiskReasonReady {
			result.ExpiresAt = riskGrantExpiry(now, result.Market, time.Duration(maximumAgeMS)*time.Millisecond)
			result.Allowed = result.ExpiresAt.After(now)
			if !result.Allowed {
				result.Reason = domain.RiskReasonMarket
				result.ExpiresAt = now.Add(time.Millisecond)
			}
		}
		return saveRiskDecision(ctx, tx, &strategy, result, maximumAgeMS)
	})
	if err != nil {
		return domain.RiskDecision{}, coreFailure(ctx, err)
	}
	return result, nil
}

func evaluateRiskInputs(ctx context.Context, tx *gorm.DB, strategy *modelv1.StrategyInstance) (domain.RiskDecision, int64, error) {
	result := domain.RiskDecision{
		StrategyID: strategy.Id,
		Reason:     domain.RiskReasonParameters,
	}
	if !requestedConfigurationActive(strategy) {
		result.Reason = domain.RiskReasonConfigurationPending
		return result, 5000, nil
	}
	age, err := riskParameterAge(ctx, tx, strategy)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidParameters) || errors.Is(err, gorm.ErrRecordNotFound) {
			return result, 5000, nil
		}
		return result, 0, err
	}
	rows, err := lockCoreRows(ctx, tx, strategy.AccountId, strategy.InstrumentId)
	if err != nil {
		return result, 0, err
	}
	result.Reason = domain.RiskReasonBudget
	if !rows.account.Enabled || rows.instrument.Status != domain.InstrumentStatusTrading || rows.instrument.ExecutionVenue != domain.ExecutionVenueSimulator {
		return result, age, nil
	}
	eligible, err := riskBudgetsEnabled(ctx, tx, strategy, rows.instrument.QuoteAsset)
	if err != nil || !eligible {
		return result, age, err
	}
	result.Reason = domain.RiskReasonUnreconciled
	result.Reconciled, err = riskPortfolioReconciled(ctx, tx, strategy, rows)
	if err != nil || !result.Reconciled {
		return result, age, err
	}
	result.Reason = domain.RiskReasonMarket
	var feed modelv1.MarketFeed
	err = tx.WithContext(ctx).Model(&modelv1.MarketFeed{}).Where("source = ? AND instrument_id = ? AND mode = ? AND delete_at IS NULL", *strategy.ActiveMarketDataSource, strategy.InstrumentId, strategy.MarketMode).First(&feed).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return result, age, nil
	}
	if err != nil {
		return result, 0, err
	}
	reference, _, err := readRiskPublication(ctx, tx, strategy, feed.Id, age)
	if err != nil {
		if err == domain.ErrRiskDenied || errors.Is(err, gorm.ErrRecordNotFound) {
			return result, age, nil
		}
		return result, 0, err
	}
	result.Market = reference
	result.Reason = domain.RiskReasonReady
	return result, age, nil
}

func riskParameterAge(ctx context.Context, tx *gorm.DB, strategy *modelv1.StrategyInstance) (int64, error) {
	var row modelv1.StrategyConfigVersion
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Where("instance_id = ? AND version = ? AND delete_at IS NULL", strategy.Id, *strategy.ActiveConfigVersion).Clauses(clause.Locking{Strength: "SHARE"}).First(&row).Error; err != nil {
		return 0, err
	}
	if row.ParametersSchemaVersion != domain.ParametersSchemaVersion || row.GetTemplate() != strategy.Template || row.MarketDataSource != *strategy.ActiveMarketDataSource || row.CodeVersion != strategy.CodeVersion {
		return 0, domain.ErrInvalidParameters
	}
	switch domain.StrategyTemplate(strategy.Template) {
	case domain.StrategyTemplateMarketMaker:
		if row.GridParameters != nil {
			return 0, domain.ErrInvalidParameters
		}
		parameters, err := strategyconfig.Maker(row.MakerParameters)
		if err != nil {
			return 0, err
		}
		return min(parameters.MaxMarketAge.Milliseconds(), int64(5000)), nil
	case domain.StrategyTemplateGrid:
		if row.MakerParameters != nil {
			return 0, domain.ErrInvalidParameters
		}
		parameters, err := strategyconfig.Grid(row.GridParameters)
		if err != nil {
			return 0, err
		}
		return min(parameters.MaxMarketAge.Milliseconds(), int64(5000)), nil
	default:
		return 0, domain.ErrInvalidParameters
	}
}

func riskEvaluationTime(ctx context.Context, tx *gorm.DB, strategyID int64) (time.Time, error) {
	var clock struct{ NowMS int64 }
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", strategyID).Select("floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint AS now_ms").Take(&clock).Error; err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(clock.NowMS).UTC(), nil
}

func riskGrantExpiry(now time.Time, reference domain.MarketReference, maximumAge time.Duration) time.Time {
	until := now.Add(domain.MaxRiskSnapshotAge)
	for _, limit := range []time.Time{reference.AuthorityUntil, reference.Reading.ValidUntil, reference.Reading.EventTime.Add(maximumAge), reference.Reading.ReceivedAt.Add(maximumAge)} {
		if limit.Before(until) {
			until = limit
		}
	}
	return until
}
