package repository

import (
	"context"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type adminPortfolioStatsRow struct {
	AccountID    string
	InstrumentID int64
	Count        int64
	Latest       int64
}

func readAdminPortfolioStats(ctx context.Context, tx *gorm.DB, rows []*modelv1.PortfolioAccount) (map[adminPortfolioKey]domain.AdminPortfolioFacts, map[adminPortfolioKey]domain.AdminPortfolioFacts, error) {
	// Only these exact account/instrument pairs qualify. The source join has
	// account_id/instrument_id only on simulator_orders; collected rows carry
	// both fields directly. Neither query materializes the full fill history.
	scope := tx.WithContext(ctx).Model(&modelv1.PortfolioAccount{}).Where("FALSE")
	for _, row := range rows {
		scope = scope.Or("account_id = ? AND instrument_id = ?", row.AccountId, row.InstrumentId)
	}
	var sources []adminPortfolioStatsRow
	if err := tx.WithContext(ctx).Model(&modelv1.SimulatorFill{}).
		Select("simulator_orders.account_id AS account_id, simulator_orders.instrument_id AS instrument_id, COUNT(*) AS count, MAX(simulator_fills.id) AS latest").
		Joins("JOIN simulator_orders ON simulator_orders.id = simulator_fills.core_order_id AND simulator_orders.delete_at IS NULL").
		Where(scope).Where("simulator_fills.delete_at IS NULL").
		Group("simulator_orders.account_id, simulator_orders.instrument_id").Scan(&sources).Error; err != nil {
		return nil, nil, err
	}
	var collected []adminPortfolioStatsRow
	if err := tx.WithContext(ctx).Model(&modelv1.PortfolioFill{}).
		Select("account_id, instrument_id, COUNT(*) AS count, MAX(source_sequence) AS latest").
		Where(scope).Where("delete_at IS NULL AND execution_venue = ?", domain.ExecutionVenueSimulator).
		Group("account_id, instrument_id").Scan(&collected).Error; err != nil {
		return nil, nil, err
	}
	return adminPortfolioStats(sources), adminPortfolioStats(collected), nil
}

func adminPortfolioStats(rows []adminPortfolioStatsRow) map[adminPortfolioKey]domain.AdminPortfolioFacts {
	result := make(map[adminPortfolioKey]domain.AdminPortfolioFacts, len(rows))
	for _, row := range rows {
		result[adminPortfolioKey{
			account:    row.AccountID,
			instrument: row.InstrumentID,
		}] = domain.AdminPortfolioFacts{
			Count:                row.Count,
			LatestSourceSequence: row.Latest,
		}
	}
	return result
}
