package repository

import (
	"context"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func lockBalances(ctx context.Context, tx *gorm.DB, rows coreRows) (*modelv1.SimulatorBalance, *modelv1.SimulatorBalance, error) {
	var balances []*modelv1.SimulatorBalance
	err := tx.WithContext(ctx).Model(&modelv1.SimulatorBalance{}).Where("account_id = ? AND asset IN ? AND delete_at IS NULL", rows.account.AccountId, []string{rows.instrument.BaseAsset, rows.instrument.QuoteAsset}).
		Order("asset ASC").Clauses(clause.Locking{Strength: "UPDATE"}).Find(&balances).Error
	if err != nil {
		return nil, nil, err
	}
	var base, quote *modelv1.SimulatorBalance
	for _, balance := range balances {
		if balance.Asset == rows.instrument.BaseAsset {
			base = balance
		}
		if balance.Asset == rows.instrument.QuoteAsset {
			quote = balance
		}
	}
	if base == nil || quote == nil || base.Id == quote.Id {
		return nil, nil, domain.ErrCoreNotFound
	}
	return base, quote, nil
}

func adjustBalance(ctx context.Context, tx *gorm.DB, row *modelv1.SimulatorBalance, availableDelta, frozenDelta decimal.Decimal) error {
	available, err := decimal.NewFromString(row.Available)
	if err != nil {
		return err
	}
	frozen, err := decimal.NewFromString(row.Frozen)
	if err != nil {
		return err
	}
	available, frozen = available.Add(availableDelta), frozen.Add(frozenDelta)
	if !domain.FixedDecimal(available, 40, 36, true) || !domain.FixedDecimal(frozen, 40, 36, true) {
		return domain.ErrInsufficientFunds
	}
	previous := row.Revision
	row.Available = available.String()
	row.Frozen = frozen.String()
	row.Revision++
	result := tx.WithContext(ctx).Model(&modelv1.SimulatorBalance{}).Where("id = ? AND revision = ?", row.Id, previous).
		Select("Available", "Frozen", "Revision").Updates(row)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return domain.ErrIdempotencyConflict
	}
	return nil
}

func advanceCoreAccount(ctx context.Context, tx *gorm.DB, row *modelv1.SimulatorAccount) error {
	previous := row.Revision
	row.Revision++
	result := tx.WithContext(ctx).Model(&modelv1.SimulatorAccount{}).Where("id = ? AND revision = ?", row.Id, previous).Select("Revision").Updates(row)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return domain.ErrIdempotencyConflict
	}
	return nil
}

func releaseCoreOrder(ctx context.Context, tx *gorm.DB, rows coreRows, row *modelv1.SimulatorOrder) error {
	base, quote, err := lockBalances(ctx, tx, rows)
	if err != nil {
		return err
	}
	frozen, err := decimal.NewFromString(row.FrozenAmount)
	if err != nil {
		return err
	}
	balance := base
	if row.Side == string(domain.SideBuy) {
		balance = quote
	}
	if err := adjustBalance(ctx, tx, balance, frozen, frozen.Neg()); err != nil {
		return err
	}
	row.FrozenAmount = "0"
	row.State = string(domain.OrderStateCanceled)
	row.CancelState = string(domain.CancelStateConfirmed)
	if err := saveCoreOrder(ctx, tx, row); err != nil {
		return err
	}
	return advanceCoreAccount(ctx, tx, rows.account)
}
