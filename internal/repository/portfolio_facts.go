package repository

import (
	"context"
	"errors"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func verifyPortfolioSource(ctx context.Context, tx *gorm.DB, portfolio *modelv1.PortfolioAccount, fact domain.FillFact) error {
	if fact.AccountID != portfolio.AccountId || fact.InstrumentID != portfolio.InstrumentId || fact.FeeAsset != portfolio.QuoteAsset {
		return domain.ErrInvalidOrder
	}
	var source modelv1.SimulatorFill
	if err := tx.WithContext(ctx).Model(&modelv1.SimulatorFill{}).Where("id = ? AND trade_id = ? AND delete_at IS NULL", fact.SourceSequence, fact.TradeID).First(&source).Error; err != nil {
		return err
	}
	if source.CoreOrderId != fact.CoreOrderID || source.FeeAsset != fact.FeeAsset ||
		!decimalTextEqual(source.Price, fact.Price) || !decimalTextEqual(source.Quantity, fact.Quantity) ||
		!decimalTextEqual(source.QuoteNotional, fact.QuoteNotional) || !decimalTextEqual(source.FeeAmount, fact.FeeAmount) {
		return domain.ErrInvalidOrder
	}
	var order modelv1.SimulatorOrder
	if err := tx.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).First(&order, fact.CoreOrderID).Error; err != nil {
		return err
	}
	var intent modelv1.OrderIntent
	if err := tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).First(&intent, fact.IntentID).Error; err != nil {
		return err
	}
	if order.AccountId != fact.AccountID || order.InstrumentId != fact.InstrumentID || order.StrategyInstanceId != fact.StrategyID || order.Side != string(fact.Side) ||
		intent.AccountId != fact.AccountID || intent.InstrumentId != fact.InstrumentID || intent.StrategyInstanceId != fact.StrategyID || intent.ClientOrderId != order.ClientOrderId {
		return domain.ErrInvalidOrder
	}
	return nil
}

func recordPortfolioInbox(ctx context.Context, tx *gorm.DB, row *modelv1.PortfolioAccount, notice domain.FillNotice) error {
	var existing modelv1.ConsumerInbox
	err := tx.WithContext(ctx).Model(&modelv1.ConsumerInbox{}).Where("consumer = ? AND event_id = ?", domain.PortfolioConsumer, notice.EventID).First(&existing).Error
	if err == nil {
		if existing.DeleteAt != nil || existing.PayloadHash != notice.PayloadHash {
			return domain.ErrIdempotencyConflict
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	now, err := portfolioClock(ctx, tx, row.Id)
	if err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&modelv1.ConsumerInbox{}).Create(&modelv1.ConsumerInbox{
		Consumer:    domain.PortfolioConsumer,
		EventId:     notice.EventID,
		PayloadHash: notice.PayloadHash,
		ProcessedAt: now,
	}).Error
}

func recordPortfolioFill(ctx context.Context, tx *gorm.DB, fact domain.FillFact) error {
	var existing modelv1.PortfolioFill
	err := tx.WithContext(ctx).Model(&modelv1.PortfolioFill{}).Where("execution_venue = ? AND trade_id = ?", fact.ExecutionVenue, fact.TradeID).First(&existing).Error
	if err == nil {
		if existing.DeleteAt != nil || !samePortfolioFact(&existing, fact) {
			return domain.ErrIdempotencyConflict
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return tx.WithContext(ctx).Model(&modelv1.PortfolioFill{}).Create(&modelv1.PortfolioFill{
		ExecutionVenue:     fact.ExecutionVenue,
		TradeId:            fact.TradeID,
		SourceSequence:     fact.SourceSequence,
		IntentId:           fact.IntentID,
		CoreOrderId:        fact.CoreOrderID,
		AccountId:          fact.AccountID,
		StrategyInstanceId: fact.StrategyID,
		InstrumentId:       fact.InstrumentID,
		Side:               string(fact.Side),
		Price:              fact.Price.String(),
		Quantity:           fact.Quantity.String(),
		QuoteNotional:      fact.QuoteNotional.String(),
		FeeAmount:          fact.FeeAmount.String(),
		FeeAsset:           fact.FeeAsset,
	}).Error
}

func samePortfolioFact(row *modelv1.PortfolioFill, fact domain.FillFact) bool {
	return row.SourceSequence == fact.SourceSequence && row.IntentId == fact.IntentID && row.CoreOrderId == fact.CoreOrderID && row.AccountId == fact.AccountID &&
		row.StrategyInstanceId == fact.StrategyID && row.InstrumentId == fact.InstrumentID && row.Side == string(fact.Side) && row.FeeAsset == fact.FeeAsset &&
		decimalTextEqual(row.Price, fact.Price) && decimalTextEqual(row.Quantity, fact.Quantity) && decimalTextEqual(row.QuoteNotional, fact.QuoteNotional) && decimalTextEqual(row.FeeAmount, fact.FeeAmount)
}

func decimalTextEqual(text string, value decimal.Decimal) bool {
	parsed, err := decimal.NewFromString(text)
	return err == nil && parsed.Equal(value)
}

func portfolioSourceStats(ctx context.Context, tx *gorm.DB, accountID string, instrumentID int64) (int64, int64, error) {
	orders := tx.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Select("id").Where("account_id = ?", accountID)
	if instrumentID > 0 {
		orders = orders.Where("instrument_id = ?", instrumentID)
	}
	var stats struct {
		Count int64
		Last  int64
	}
	err := tx.WithContext(ctx).Model(&modelv1.SimulatorFill{}).Where("core_order_id IN (?)", orders).
		Select("COUNT(*) AS count, COALESCE(MAX(id), 0) AS last").Scan(&stats).Error
	return stats.Count, stats.Last, err
}

func portfolioClock(ctx context.Context, tx *gorm.DB, id int64) (int64, error) {
	var clock struct{ NowMS int64 }
	err := tx.WithContext(ctx).Model(&modelv1.PortfolioAccount{}).Where("id = ?", id).
		Select("floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint AS now_ms").Take(&clock).Error
	return clock.NowMS, err
}
