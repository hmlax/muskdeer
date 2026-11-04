package repository

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	commonmodelv1 "github.com/hmlax/muskdeer/deer/proto/common/model/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/proto/execution/model/v1"
	portfoliomodelv1 "github.com/hmlax/muskdeer/deer/proto/portfolio/model/v1"
	strategymodelv1 "github.com/hmlax/muskdeer/deer/proto/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func adminFlattenBasis(ctx context.Context, tx *gorm.DB, instance *strategymodelv1.StrategyInstance, core coreRows, account *commonmodelv1.AccountBudget) (domain.AdminFlattenBasis, error) {
	if !core.account.Enabled || core.instrument.Status != domain.InstrumentStatusTrading || account.Asset != core.instrument.QuoteAsset {
		return domain.AdminFlattenBasis{}, domain.ErrAdminFlattenDenied
	}
	var budget strategymodelv1.StrategyBudget
	if err := tx.WithContext(ctx).Model(&strategymodelv1.StrategyBudget{}).Where("strategy_instance_id = ? AND account_id = ? AND delete_at IS NULL", instance.Id, account.AccountId).
		Clauses(clause.Locking{Strength: "SHARE"}).Take(&budget).Error; err != nil {
		return domain.AdminFlattenBasis{}, err
	}
	var portfolio portfoliomodelv1.PortfolioAccount
	if err := tx.WithContext(ctx).Model(&portfoliomodelv1.PortfolioAccount{}).Where("account_id = ? AND delete_at IS NULL", account.AccountId).
		Clauses(clause.Locking{Strength: "SHARE"}).Take(&portfolio).Error; err != nil {
		return domain.AdminFlattenBasis{}, err
	}
	base, quote, err := lockBalances(ctx, tx, core)
	if err != nil {
		return domain.AdminFlattenBasis{}, err
	}
	basis, err := flattenBasisValues(&portfolio, base, quote, core.instrument, account, &budget)
	if err != nil {
		return domain.AdminFlattenBasis{}, err
	}
	var siblings []*strategymodelv1.StrategyInstance
	if err := tx.WithContext(ctx).Model(&strategymodelv1.StrategyInstance{}).Select("id").Where("account_id = ? AND delete_at IS NULL", account.AccountId).Limit(2).Find(&siblings).Error; err != nil {
		return domain.AdminFlattenBasis{}, err
	}
	basis.StrategyCount = int64(len(siblings))
	basis.StrategyID = instance.Id
	basis.RiskRevision, err = adminRiskRevision(ctx, tx, instance.Id)
	if err != nil {
		return domain.AdminFlattenBasis{}, err
	}
	complete, err := riskPortfolioReconciled(ctx, tx, instance, core)
	if err != nil {
		return domain.AdminFlattenBasis{}, err
	}
	basis.SourceComplete = complete
	pending, err := flattenAccountObligations(ctx, tx, account.AccountId)
	if err != nil {
		return domain.AdminFlattenBasis{}, err
	}
	if pending {
		return domain.AdminFlattenBasis{}, domain.ErrAdminFlattenDenied
	}
	now, err := portfolioClock(ctx, tx, portfolio.Id)
	if err != nil {
		return domain.AdminFlattenBasis{}, err
	}
	basis.ObservedAt = time.UnixMilli(now).UTC()
	return basis, nil
}

func flattenBasisValues(portfolio *portfoliomodelv1.PortfolioAccount, base, quote *executionmodelv1.SimulatorBalance, instrument *commonmodelv1.Instrument, account *commonmodelv1.AccountBudget, budget *strategymodelv1.StrategyBudget) (domain.AdminFlattenBasis, error) {
	snapshot, err := portfolioSnapshot(portfolio)
	if err != nil {
		return domain.AdminFlattenBasis{}, err
	}
	baseAvailable, aErr := domain.ParseLedgerDecimal(base.Available, false, true)
	baseFrozen, fErr := domain.ParseLedgerDecimal(base.Frozen, false, true)
	quoteFrozen, qErr := domain.ParseLedgerDecimal(quote.Frozen, false, true)
	accountReserved, arErr := domain.ParseLedgerDecimal(account.ReservedNotional, false, true)
	strategyReserved, srErr := domain.ParseLedgerDecimal(budget.ReservedNotional, false, true)
	accountLimit, alErr := domain.ParseLedgerDecimal(account.LimitNotional, false, false)
	strategyLimit, slErr := domain.ParseLedgerDecimal(budget.LimitNotional, false, false)
	tick, tErr := domain.ParseDecimal(instrument.PriceTick, flattenOrderDecimalRule())
	lot, lErr := domain.ParseDecimal(instrument.QuantityStep, flattenOrderDecimalRule())
	minimum, mErr := domain.ParseDecimal(instrument.MinNotional, flattenOrderDecimalRule())
	if errors.Join(aErr, fErr, qErr, arErr, srErr, alErr, slErr, tErr, lErr, mErr) != nil {
		return domain.AdminFlattenBasis{}, domain.ErrAdminUnavailable
	}
	if !quoteFrozen.IsZero() || !accountReserved.IsZero() || !strategyReserved.IsZero() {
		return domain.AdminFlattenBasis{}, domain.ErrAdminFlattenDenied
	}
	return domain.AdminFlattenBasis{
		Role:                 domain.AdminRoleAdmin,
		AccountID:            account.AccountId,
		InstrumentID:         instrument.Id,
		ExecutionVenue:       instrument.ExecutionVenue,
		RiskEnabled:          budget.Enabled,
		PortfolioRevision:    portfolio.Revision,
		BaseRevision:         base.Revision,
		ProjectionReconciled: snapshot.Reconciled,
		PortfolioQuantity:    snapshot.Values.Quantity,
		CoreAvailable:        baseAvailable,
		CoreFrozen:           baseFrozen,
		TickSize:             tick,
		LotSize:              lot,
		MinQuantity:          lot,
		MinNotional:          minimum,
		MaxOrderNotional:     decimal.Min(accountLimit, strategyLimit),
	}, nil
}

func flattenOrderDecimalRule() domain.DecimalRule {
	return domain.DecimalRule{
		IntegerDigits: 20,
		Scale:         18,
	}
}

// Any unresolved account obligation blocks a new proposal, even when the
// original strategy's scope has historical final proof. Counts are never added.
func flattenAccountObligations(ctx context.Context, tx *gorm.DB, accountID string) (bool, error) {
	var count int64
	if err := tx.WithContext(ctx).Model(&executionmodelv1.OrderIntent{}).Where("account_id = ? AND delete_at IS NULL", accountID).
		Where("(state IN ? OR reserved_quote_notional <> 0)", []domain.OrderState{domain.OrderStateIntent, domain.OrderStateSubmitting, domain.OrderStateLive, domain.OrderStatePartiallyFilled, domain.OrderStateUnknown}).Count(&count).Error; err != nil || count != 0 {
		return count != 0, err
	}
	if err := tx.WithContext(ctx).Model(&executionmodelv1.SimulatorOrder{}).Where("account_id = ? AND delete_at IS NULL AND state IN ?", accountID,
		[]domain.OrderState{domain.OrderStateLive, domain.OrderStatePartiallyFilled}).Count(&count).Error; err != nil || count != 0 {
		return count != 0, err
	}
	if err := tx.WithContext(ctx).Model(&strategymodelv1.StrategyAction{}).Where("account_id = ? AND delete_at IS NULL AND state IN ?", accountID,
		[]domain.StrategyActionState{domain.StrategyActionPending, domain.StrategyActionDispatching, domain.StrategyActionWaiting, domain.StrategyActionUnknown}).Count(&count).Error; err != nil || count != 0 {
		return count != 0, err
	}
	if err := tx.WithContext(ctx).Model(&executionmodelv1.CancellationRequest{}).Where("account_id = ? AND delete_at IS NULL AND state IN ?", accountID,
		[]domain.CancellationState{domain.CancellationRequested, domain.CancellationSubmitting, domain.CancellationPending, domain.CancellationUnknown}).Count(&count).Error; err != nil || count != 0 {
		return count != 0, err
	}
	return false, nil
}

func flattenCancellationProof(ctx context.Context, tx *gorm.DB, strategyID, id int64) (domain.AdminEmergencyCancelRecord, error) {
	var row executionmodelv1.AdminEmergencyCancellation
	if err := tx.WithContext(ctx).Model(&executionmodelv1.AdminEmergencyCancellation{}).Where("id = ? AND strategy_instance_id = ? AND delete_at IS NULL", id, strategyID).Take(&row).Error; err != nil {
		return domain.AdminEmergencyCancelRecord{}, adminRiskLookupFailure(err)
	}
	var task executionmodelv1.AdminEmergencyCancellationTask
	if err := tx.WithContext(ctx).Model(&executionmodelv1.AdminEmergencyCancellationTask{}).Where("emergency_cancellation_id = ? AND strategy_instance_id = ? AND delete_at IS NULL", id, strategyID).Take(&task).Error; err != nil {
		return domain.AdminEmergencyCancelRecord{}, emergencyTaskLookupFailure(err)
	}
	return adminEmergencyRecord(&row, &task)
}
