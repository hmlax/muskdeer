package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type adminPortfolios struct{ snapshots adminStrategies }

// NewAdminPortfolios borrows storage without initializing or rebuilding accounts.
func NewAdminPortfolios(db *gorm.DB) (domain.AdminPortfolios, error) {
	if db == nil {
		return nil, domain.ErrInvalidAdminQuery
	}
	return &adminPortfolios{snapshots: adminStrategies{db: db}}, nil
}

func (s *adminPortfolios) List(ctx context.Context, token string, filter domain.AdminPortfolioFilter) (domain.AdminPortfolioPage, error) {
	if err := filter.Validate(); err != nil {
		return domain.AdminPortfolioPage{}, err
	}
	var result domain.AdminPortfolioPage
	err := s.snapshots.snapshot(ctx, token, func(tx *gorm.DB) error {
		if err := adminPortfolioFilterExists(ctx, tx, filter); err != nil {
			return err
		}
		query := tx.WithContext(ctx).Model(&modelv1.PortfolioAccount{}).
			Where("id > ? AND delete_at IS NULL", filter.Page.AfterID)
		if filter.AccountID != "" {
			query = query.Where("account_id = ?", filter.AccountID)
		}
		if filter.InstrumentID != 0 {
			query = query.Where("instrument_id = ?", filter.InstrumentID)
		}
		var rows []*modelv1.PortfolioAccount
		if err := query.Order("id ASC").Limit(filter.Page.Limit + 1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > filter.Page.Limit {
			rows = rows[:filter.Page.Limit]
			result.NextAfterID = rows[len(rows)-1].Id
		}
		values, err := readAdminPortfolios(ctx, tx, rows)
		result.Items = values
		return err
	})
	if err != nil {
		return domain.AdminPortfolioPage{}, err
	}
	return result, nil
}

func (s *adminPortfolios) Detail(ctx context.Context, token string, id int64) (domain.AdminPortfolio, error) {
	if id <= 0 {
		return domain.AdminPortfolio{}, domain.ErrInvalidAdminQuery
	}
	var result domain.AdminPortfolio
	err := s.snapshots.snapshot(ctx, token, func(tx *gorm.DB) error {
		var row modelv1.PortfolioAccount
		if err := tx.WithContext(ctx).Model(&modelv1.PortfolioAccount{}).
			Where("id = ? AND delete_at IS NULL", id).Take(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrAdminNotFound
			}
			return err
		}
		values, err := readAdminPortfolios(ctx, tx, []*modelv1.PortfolioAccount{&row})
		if err != nil {
			return err
		}
		result = values[0]
		return nil
	})
	if err != nil {
		return domain.AdminPortfolio{}, err
	}
	return result, nil
}

func adminPortfolioFilterExists(ctx context.Context, tx *gorm.DB, filter domain.AdminPortfolioFilter) error {
	if filter.AccountID != "" {
		var row modelv1.SimulatorAccount
		if err := tx.WithContext(ctx).Model(&modelv1.SimulatorAccount{}).
			Select("Id").Where("account_id = ? AND delete_at IS NULL", filter.AccountID).Take(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrAdminNotFound
			}
			return err
		}
	}
	if filter.InstrumentID != 0 {
		var row modelv1.Instrument
		if err := tx.WithContext(ctx).Model(&modelv1.Instrument{}).
			Select("Id").Where("id = ? AND delete_at IS NULL", filter.InstrumentID).Take(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrAdminNotFound
			}
			return err
		}
	}
	return nil
}

var _ domain.AdminPortfolios = (*adminPortfolios)(nil)
