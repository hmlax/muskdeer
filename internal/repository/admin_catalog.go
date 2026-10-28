package repository

import (
	"context"
	"errors"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type adminCatalog struct{ snapshots adminStrategies }

// NewAdminCatalog borrows storage without provisioning or granting resources.
func NewAdminCatalog(db *gorm.DB) (domain.AdminCatalog, error) {
	if db == nil {
		return nil, domain.ErrInvalidAdminQuery
	}
	return &adminCatalog{snapshots: adminStrategies{db: db}}, nil
}

func (s *adminCatalog) Accounts(ctx context.Context, token string, page domain.AdminPage) (domain.AdminAccountPage, error) {
	if !validAdminPage(page) {
		return domain.AdminAccountPage{}, domain.ErrInvalidAdminQuery
	}
	result := domain.AdminAccountPage{Items: make([]domain.AdminAccount, 0, page.Limit)}
	err := s.snapshots.snapshot(ctx, token, func(tx *gorm.DB) error {
		var rows []*modelv1.SimulatorAccount
		if err := tx.WithContext(ctx).Model(&modelv1.SimulatorAccount{}).
			Where("id > ? AND delete_at IS NULL", page.AfterID).Order("id ASC").Limit(page.Limit + 1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > page.Limit {
			rows = rows[:page.Limit]
			result.NextAfterID = rows[len(rows)-1].Id
		}
		for _, row := range rows {
			fee, err := domain.ParseDecimal(row.QuoteFeeRate, domain.DecimalRule{
				IntegerDigits: 1,
				Scale:         18,
				AllowZero:     true,
			})
			if err != nil {
				return err
			}
			if fee.GreaterThanOrEqual(decimal.NewFromInt(1)) {
				return domain.ErrInvalidDecimal
			}
			result.Items = append(result.Items, domain.AdminAccount{
				ID:           row.Id,
				AccountID:    row.AccountId,
				SubjectID:    row.SubjectId,
				Enabled:      row.Enabled,
				QuoteFeeRate: fee,
				Revision:     row.Revision,
			})
		}
		return nil
	})
	if err != nil {
		return domain.AdminAccountPage{}, err
	}
	return result, nil
}

func (s *adminCatalog) Instruments(ctx context.Context, token string, page domain.AdminPage) (domain.AdminInstrumentPage, error) {
	if !validAdminPage(page) {
		return domain.AdminInstrumentPage{}, domain.ErrInvalidAdminQuery
	}
	result := domain.AdminInstrumentPage{Items: make([]domain.AdminInstrument, 0, page.Limit)}
	err := s.snapshots.snapshot(ctx, token, func(tx *gorm.DB) error {
		var rows []*modelv1.Instrument
		if err := tx.WithContext(ctx).Model(&modelv1.Instrument{}).
			Where("id > ? AND delete_at IS NULL", page.AfterID).Order("id ASC").Limit(page.Limit + 1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > page.Limit {
			rows = rows[:page.Limit]
			result.NextAfterID = rows[len(rows)-1].Id
		}
		for _, row := range rows {
			rules, err := strategyInstrumentValue(row)
			if err != nil {
				return err
			}
			result.Items = append(result.Items, domain.AdminInstrument{
				Rules:          rules,
				ExecutionVenue: row.ExecutionVenue,
				Symbol:         row.Symbol,
				BaseAsset:      row.BaseAsset,
				QuoteAsset:     row.QuoteAsset,
			})
		}
		return nil
	})
	if err != nil {
		return domain.AdminInstrumentPage{}, err
	}
	return result, nil
}

func (s *adminCatalog) Feeds(ctx context.Context, token string, instrumentID int64, mode domain.MarketMode, page domain.AdminPage) (domain.AdminFeedPage, error) {
	if instrumentID <= 0 || (mode != domain.MarketModeLive && mode != domain.MarketModeReplay) || !validAdminPage(page) {
		return domain.AdminFeedPage{}, domain.ErrInvalidAdminQuery
	}
	result := domain.AdminFeedPage{Items: make([]domain.MarketFeedDefinition, 0, page.Limit)}
	err := s.snapshots.snapshot(ctx, token, func(tx *gorm.DB) error {
		var instrument modelv1.Instrument
		if err := tx.WithContext(ctx).Model(&modelv1.Instrument{}).Where("id = ? AND delete_at IS NULL", instrumentID).Take(&instrument).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrAdminNotFound
			}
			return err
		}
		var rows []*modelv1.MarketFeed
		if err := tx.WithContext(ctx).Model(&modelv1.MarketFeed{}).
			Where("id > ? AND instrument_id = ? AND mode = ? AND delete_at IS NULL", page.AfterID, instrumentID, mode).
			Order("id ASC").Limit(page.Limit + 1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > page.Limit {
			rows = rows[:page.Limit]
			result.NextAfterID = rows[len(rows)-1].Id
		}
		for _, row := range rows {
			feed, err := marketFeedValue(row)
			if err != nil {
				return err
			}
			result.Items = append(result.Items, feed)
		}
		return nil
	})
	if err != nil {
		return domain.AdminFeedPage{}, err
	}
	return result, nil
}
