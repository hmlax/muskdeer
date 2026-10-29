package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategyconfig"
)

type strategyEnvironment struct{ db *gorm.DB }

// NewStrategyEnvironment borrows storage without claiming or changing state.
func NewStrategyEnvironment(db *gorm.DB) (domain.StrategyEnvironment, error) {
	if db == nil {
		return nil, domain.ErrInvalidOrder
	}
	return &strategyEnvironment{db: db}, nil
}

func (s *strategyEnvironment) Current(ctx context.Context, spec domain.StrategySpec) (domain.StrategyEnvironmentView, error) {
	if err := coreContext(ctx); err != nil {
		return domain.StrategyEnvironmentView{}, err
	}
	if spec.ID <= 0 || spec.Revision <= 0 || spec.ActiveVersion <= 0 || spec.ActiveSource.ValidateSelection() != nil {
		return domain.StrategyEnvironmentView{}, domain.ErrInvalidOrder
	}
	var result domain.StrategyEnvironmentView
	err := s.db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
		var row modelv1.StrategyInstance
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ? AND delete_at IS NULL", spec.ID).First(&row).Error; err != nil {
			return err
		}
		if strategySpec(&row) != spec {
			return domain.ErrIdempotencyConflict
		}
		var err error
		result, err = readStrategyEnvironment(ctx, tx, &row)
		return err
	}, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if errors.Is(err, domain.ErrInvalidParameters) {
		return domain.StrategyEnvironmentView{}, err
	}
	if err != nil {
		return domain.StrategyEnvironmentView{}, coreFailure(ctx, err)
	}
	return result, nil
}

func readStrategyEnvironment(ctx context.Context, tx *gorm.DB, strategy *modelv1.StrategyInstance) (domain.StrategyEnvironmentView, error) {
	var result domain.StrategyEnvironmentView
	var instrument modelv1.Instrument
	if err := tx.WithContext(ctx).Model(&modelv1.Instrument{}).Where("id = ? AND delete_at IS NULL", strategy.InstrumentId).First(&instrument).Error; err != nil {
		return result, err
	}
	var account modelv1.SimulatorAccount
	if err := tx.WithContext(ctx).Model(&modelv1.SimulatorAccount{}).Where("account_id = ? AND delete_at IS NULL", strategy.AccountId).First(&account).Error; err != nil {
		return result, err
	}
	if !account.Enabled || instrument.ExecutionVenue != domain.ExecutionVenueSimulator || instrument.Status != domain.InstrumentStatusTrading {
		return result, domain.ErrRiskDenied
	}
	var feed modelv1.MarketFeed
	if err := tx.WithContext(ctx).Model(&modelv1.MarketFeed{}).
		Where("source = ? AND instrument_id = ? AND mode = ? AND delete_at IS NULL", strategy.GetActiveMarketDataSource(), strategy.InstrumentId, strategy.MarketMode).First(&feed).Error; err != nil {
		return result, err
	}
	result.Symbol, result.BaseAsset, result.QuoteAsset = feed.ExternalSymbol, instrument.BaseAsset, instrument.QuoteAsset
	var err error
	result.Instrument, err = strategyInstrumentValue(&instrument)
	if err != nil {
		return result, err
	}
	result.QuoteFeeRate, err = domain.ParseDecimal(account.QuoteFeeRate, domain.DecimalRule{
		IntegerDigits: 1,
		Scale:         18,
		AllowZero:     true,
	})
	if err != nil || result.QuoteFeeRate.GreaterThanOrEqual(decimal.NewFromInt(1)) {
		return result, domain.ErrInvalidOrder
	}
	result.Parameters, err = readStrategyParameters(ctx, tx, strategy)
	if err != nil {
		return result, err
	}
	if err := strategyPlanningFunds(ctx, tx, strategy, &result); err != nil {
		return result, err
	}
	result.TotalNotional, result.MaximumOrderNotional, err = strategyPlanningBudget(ctx, tx, strategy, instrument.QuoteAsset)
	if err != nil {
		return result, err
	}
	result.LocalBid, err = strategyLocalPrice(ctx, tx, instrument.Id, domain.SideBuy, "price DESC")
	if err != nil {
		return result, err
	}
	result.LocalAsk, err = strategyLocalPrice(ctx, tx, instrument.Id, domain.SideSell, "price ASC")
	if err != nil {
		return result, err
	}
	result.ObservedAt, err = riskEvaluationTime(ctx, tx, strategy.Id)
	if err != nil {
		return result, err
	}
	var actions []*modelv1.StrategyAction
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyAction{}).Select("CreateAt").
		Where("strategy_instance_id = ? AND delete_at IS NULL", strategy.Id).Order("id DESC").Limit(1).Find(&actions).Error; err != nil {
		return result, err
	}
	if len(actions) != 0 {
		result.LastRefresh = time.UnixMilli(actions[0].CreateAt).UTC()
	}
	result.RiskAllowed, result.RiskReason, err = strategyPlanningRisk(ctx, tx, strategy, result.ObservedAt)
	return result, err
}

func readStrategyParameters(ctx context.Context, tx *gorm.DB, strategy *modelv1.StrategyInstance) (domain.StrategyParameters, error) {
	var row modelv1.StrategyConfigVersion
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).
		Where("instance_id = ? AND version = ? AND delete_at IS NULL", strategy.Id, strategy.GetActiveConfigVersion()).First(&row).Error; err != nil {
		return domain.StrategyParameters{}, err
	}
	result := domain.StrategyParameters{Template: domain.StrategyTemplate(strategy.Template)}
	if row.ParametersSchemaVersion != domain.ParametersSchemaVersion || row.GetTemplate() != strategy.Template ||
		row.CodeVersion != strategy.CodeVersion || row.MarketDataSource != strategy.GetActiveMarketDataSource() {
		return result, domain.ErrInvalidParameters
	}
	switch result.Template {
	case domain.StrategyTemplateMarketMaker:
		if row.GridParameters != nil {
			return result, domain.ErrInvalidParameters
		}
		value, err := strategyconfig.Maker(row.MakerParameters)
		result.Maker = &value
		return result, err
	case domain.StrategyTemplateGrid:
		if row.MakerParameters != nil {
			return result, domain.ErrInvalidParameters
		}
		value, err := strategyconfig.Grid(row.GridParameters)
		result.Grid = &value
		return result, err
	default:
		return result, domain.ErrInvalidParameters
	}
}

func strategyInstrumentValue(row *modelv1.Instrument) (domain.StrategyInstrument, error) {
	result := domain.StrategyInstrument{
		ID:              row.Id,
		MetadataVersion: row.MetadataVersion,
		Status:          row.Status,
	}
	for _, field := range []struct {
		text string
		into *decimal.Decimal
	}{
		{
			text: row.PriceTick,
			into: &result.PriceTick,
		},
		{
			text: row.QuantityStep,
			into: &result.QuantityStep,
		},
		{
			text: row.MinNotional,
			into: &result.MinNotional,
		},
	} {
		value, err := domain.ParseDecimal(field.text, domain.DecimalRule{
			IntegerDigits: 20,
			Scale:         18,
		})
		if err != nil {
			return result, err
		}
		*field.into = value
	}
	return result, nil
}

func strategyLocalPrice(ctx context.Context, tx *gorm.DB, instrumentID int64, side domain.Side, ordering string) (decimal.Decimal, error) {
	var rows []*modelv1.SimulatorOrder
	if err := tx.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Select("Price").
		Where("instrument_id = ? AND side = ? AND state IN ? AND delete_at IS NULL", instrumentID, side, []domain.OrderState{domain.OrderStateLive, domain.OrderStatePartiallyFilled}).
		Order(ordering).Limit(1).Find(&rows).Error; err != nil {
		return decimal.Zero, err
	}
	if len(rows) == 0 {
		return decimal.Zero, nil
	}
	return domain.ParseDecimal(rows[0].Price, domain.DecimalRule{
		IntegerDigits: 20,
		Scale:         18,
	})
}

func strategyPlanningRisk(ctx context.Context, tx *gorm.DB, strategy *modelv1.StrategyInstance, now time.Time) (bool, domain.RiskReason, error) {
	if !requestedConfigurationActive(strategy) {
		return false, domain.RiskReasonConfigurationPending, nil
	}
	var row modelv1.RiskSnapshot
	err := tx.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ? AND delete_at IS NULL", strategy.Id).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, domain.RiskReasonNotEvaluated, nil
	}
	if err != nil {
		return false, domain.RiskReasonNotEvaluated, err
	}
	allowed := row.AllowNewRisk && row.Reconciled && row.ConfigVersion == strategy.GetActiveConfigVersion() &&
		row.MarketDataSource == strategy.GetActiveMarketDataSource() && row.AccountId == strategy.AccountId && row.InstrumentId == strategy.InstrumentId &&
		row.ObservedAt <= now.UnixMilli() && row.ExpiresAt > now.UnixMilli() && now.UnixMilli()-row.ObservedAt <= domain.MaxRiskSnapshotAge.Milliseconds()
	return allowed, domain.RiskReason(row.ReasonCode), nil
}
