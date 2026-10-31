package repository

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func validateAdminRiskRecovery(ctx context.Context, tx *gorm.DB, instance *modelv1.StrategyInstance, budget *modelv1.StrategyBudget, instrument *modelv1.Instrument) (time.Time, error) {
	reserved, err := domain.ParseLedgerDecimal(budget.ReservedNotional, false, true)
	if err != nil {
		return time.Time{}, err
	}
	if !reserved.IsZero() {
		return time.Time{}, domain.ErrAdminRiskRecoveryDenied
	}
	if err := requireEmergencyScopeClear(ctx, tx, instance.Id); err != nil {
		return time.Time{}, err
	}
	if err := requireStrategyClear(ctx, tx, instance.Id); err != nil {
		return time.Time{}, adminRiskRecoveryFailure(err)
	}
	var pending int64
	if err := tx.WithContext(ctx).Model(&modelv1.CancellationRequest{}).
		Where("strategy_instance_id = ? AND delete_at IS NULL AND state IN ?", instance.Id,
			[]domain.CancellationState{domain.CancellationRequested, domain.CancellationSubmitting, domain.CancellationPending, domain.CancellationUnknown}).Count(&pending).Error; err != nil {
		return time.Time{}, err
	}
	if pending != 0 {
		return time.Time{}, domain.ErrAdminRiskRecoveryDenied
	}
	// Evaluate exactly the currently requested pair for a future activation.
	// This owned protobuf clone is never saved and cannot activate/start it.
	candidate := proto.CloneOf(instance)
	candidate.ActiveConfigVersion = proto.Int64(instance.RequestedConfigVersion)
	candidate.ActiveMarketDataSource = proto.String(instance.MarketDataSource)
	decision, age, err := evaluateRiskInputs(ctx, tx, candidate)
	if err != nil {
		return time.Time{}, adminRiskRecoveryFailure(err)
	}
	if decision.Reason != domain.RiskReasonReady || !decision.Reconciled {
		return time.Time{}, domain.ErrAdminRiskRecoveryDenied
	}
	if err := validateRiskRecoveryInventory(ctx, tx, candidate, instrument); err != nil {
		return time.Time{}, adminRiskRecoveryFailure(err)
	}
	now, err := riskEvaluationTime(ctx, tx, instance.Id)
	if err != nil {
		return time.Time{}, err
	}
	until := riskGrantExpiry(now, decision.Market, time.Duration(age)*time.Millisecond)
	if !until.After(now) {
		return time.Time{}, domain.ErrAdminRiskRecoveryDenied
	}
	return until, nil
}

func validateRiskRecoveryInventory(ctx context.Context, tx *gorm.DB, candidate *modelv1.StrategyInstance, instrument *modelv1.Instrument) error {
	var version modelv1.StrategyConfigVersion
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).
		Where("instance_id = ? AND version = ? AND delete_at IS NULL", candidate.Id, candidate.RequestedConfigVersion).First(&version).Error; err != nil {
		return err
	}
	limit, err := inventoryParameterLimit(&version)
	if err != nil {
		return err
	}
	var active int64
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).
		Where("account_id = ? AND instrument_id = ? AND active_config_version IS NOT NULL AND delete_at IS NULL", candidate.AccountId, candidate.InstrumentId).Count(&active).Error; err != nil {
		return err
	}
	if active > 0 {
		shared, err := strictestInventoryLimit(ctx, tx, candidate.AccountId, candidate.InstrumentId)
		if err != nil {
			return err
		}
		limit = decimal.Min(limit, shared)
	}
	return validateInventoryAtLimit(ctx, tx, candidate.AccountId, instrument, decimal.Zero, limit)
}

func adminRiskRecoveryFailure(err error) error {
	for _, denied := range []error{domain.ErrRiskDenied, domain.ErrOutcomeUnknown, domain.ErrInvalidParameters, gorm.ErrRecordNotFound} {
		if errors.Is(err, denied) {
			return domain.ErrAdminRiskRecoveryDenied
		}
	}
	return err
}
