package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategyconfig"
)

type adminStrategies struct{ db *gorm.DB }

// NewAdminStrategies borrows storage without starting work or granting rights.
func NewAdminStrategies(db *gorm.DB) (domain.AdminStrategies, error) {
	if db == nil {
		return nil, domain.ErrInvalidAdminQuery
	}
	return &adminStrategies{db: db}, nil
}

func (s *adminStrategies) List(ctx context.Context, token string, page domain.AdminPage) (domain.AdminStrategyPage, error) {
	if !validAdminPage(page) {
		return domain.AdminStrategyPage{}, domain.ErrInvalidAdminQuery
	}
	result := domain.AdminStrategyPage{Items: make([]domain.AdminStrategy, 0, page.Limit)}
	err := s.snapshot(ctx, token, func(tx *gorm.DB) error {
		var rows []*modelv1.StrategyInstance
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).
			Where("id > ? AND delete_at IS NULL", page.AfterID).Order("id ASC").Limit(page.Limit + 1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > page.Limit {
			rows = rows[:page.Limit]
			result.NextAfterID = rows[len(rows)-1].Id
		}
		for _, row := range rows {
			result.Items = append(result.Items, adminStrategy(row))
		}
		return nil
	})
	if err != nil {
		return domain.AdminStrategyPage{}, err
	}
	return result, nil
}

func (s *adminStrategies) Detail(ctx context.Context, token string, id int64) (domain.AdminStrategyDetail, error) {
	if id <= 0 {
		return domain.AdminStrategyDetail{}, domain.ErrInvalidAdminQuery
	}
	var result domain.AdminStrategyDetail
	err := s.snapshot(ctx, token, func(tx *gorm.DB) error {
		row, err := adminStrategyRow(ctx, tx, id)
		if err != nil {
			return err
		}
		result.Strategy = adminStrategy(row)
		result.Requested, err = adminVersion(ctx, tx, row, row.RequestedConfigVersion, row.MarketDataSource)
		if err != nil {
			return err
		}
		if row.ActiveConfigVersion != nil && row.ActiveMarketDataSource != nil {
			active, err := adminVersion(ctx, tx, row, *row.ActiveConfigVersion, *row.ActiveMarketDataSource)
			if err != nil {
				return err
			}
			result.Active = &active
		}
		return nil
	})
	if err != nil {
		return domain.AdminStrategyDetail{}, err
	}
	return result, nil
}

func (s *adminStrategies) Versions(ctx context.Context, token string, id int64, page domain.AdminPage) (domain.AdminConfigPage, error) {
	if id <= 0 || !validAdminPage(page) {
		return domain.AdminConfigPage{}, domain.ErrInvalidAdminQuery
	}
	result := domain.AdminConfigPage{Items: make([]domain.AdminConfigVersion, 0, page.Limit)}
	err := s.snapshot(ctx, token, func(tx *gorm.DB) error {
		instance, err := adminStrategyRow(ctx, tx, id)
		if err != nil {
			return err
		}
		var rows []*modelv1.StrategyConfigVersion
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).
			Where("instance_id = ? AND id > ? AND delete_at IS NULL", id, page.AfterID).
			Order("id ASC").Limit(page.Limit + 1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > page.Limit {
			rows = rows[:page.Limit]
			result.NextAfterID = rows[len(rows)-1].Id
		}
		for _, row := range rows {
			version, err := adminConfigVersion(row, instance)
			if err != nil {
				return err
			}
			result.Items = append(result.Items, version)
		}
		return nil
	})
	if err != nil {
		return domain.AdminConfigPage{}, err
	}
	return result, nil
}

func (s *adminStrategies) snapshot(ctx context.Context, token string, read func(*gorm.DB) error) error {
	if err := coreContext(ctx); err != nil {
		return err
	}
	err := s.db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
		if _, _, err := readAdminAuthority(ctx, tx, token, "", domain.AdminRoleViewer, ""); err != nil {
			return err
		}
		return read(tx)
	}, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	return adminQueryFailure(err)
}

func validAdminPage(page domain.AdminPage) bool {
	return page.AfterID >= 0 && page.Limit >= 1 && page.Limit <= domain.MaxAdminPageSize
}

func adminStrategyRow(ctx context.Context, tx *gorm.DB, id int64) (*modelv1.StrategyInstance, error) {
	var row modelv1.StrategyInstance
	err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ? AND delete_at IS NULL", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrAdminNotFound
	}
	return &row, err
}

func adminStrategy(row *modelv1.StrategyInstance) domain.AdminStrategy {
	return domain.AdminStrategy{
		Spec:      strategySpec(row),
		CreatedAt: time.UnixMilli(row.CreateAt).UTC(),
		UpdatedAt: time.UnixMilli(row.UpdateAt).UTC(),
	}
}

func adminVersion(ctx context.Context, tx *gorm.DB, instance *modelv1.StrategyInstance, version int64, source string) (domain.AdminConfigVersion, error) {
	var row modelv1.StrategyConfigVersion
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).
		Where("instance_id = ? AND version = ? AND market_data_source = ? AND delete_at IS NULL", instance.Id, version, source).Take(&row).Error; err != nil {
		return domain.AdminConfigVersion{}, err
	}
	return adminConfigVersion(&row, instance)
}

func adminConfigVersion(row *modelv1.StrategyConfigVersion, instance *modelv1.StrategyInstance) (domain.AdminConfigVersion, error) {
	result := domain.AdminConfigVersion{
		ID:            row.Id,
		StrategyID:    row.InstanceId,
		Version:       row.Version,
		Source:        domain.MarketSource(row.MarketDataSource),
		CodeVersion:   row.CodeVersion,
		CreatedBy:     row.CreatedBy,
		SchemaVersion: row.ParametersSchemaVersion,
		CreatedAt:     time.UnixMilli(row.CreateAt).UTC(),
	}
	if row.ParametersSchemaVersion == 0 && row.Template == nil && row.MakerParameters == nil && row.GridParameters == nil {
		return result, nil
	}
	if row.ParametersSchemaVersion != domain.ParametersSchemaVersion || row.GetTemplate() != instance.Template {
		return domain.AdminConfigVersion{}, domain.ErrAdminUnavailable
	}
	var err error
	switch domain.StrategyTemplate(row.GetTemplate()) {
	case domain.StrategyTemplateMarketMaker:
		if row.MakerParameters == nil || row.GridParameters != nil {
			return domain.AdminConfigVersion{}, domain.ErrAdminUnavailable
		}
		parameters, decodeErr := strategyconfig.Maker(row.MakerParameters)
		err = decodeErr
		result.Parameters = domain.StrategyParameters{
			Template: domain.StrategyTemplateMarketMaker,
			Maker:    &parameters,
		}
	case domain.StrategyTemplateGrid:
		if row.GridParameters == nil || row.MakerParameters != nil {
			return domain.AdminConfigVersion{}, domain.ErrAdminUnavailable
		}
		parameters, decodeErr := strategyconfig.Grid(row.GridParameters)
		err = decodeErr
		result.Parameters = domain.StrategyParameters{
			Template: domain.StrategyTemplateGrid,
			Grid:     &parameters,
		}
	default:
		return domain.AdminConfigVersion{}, domain.ErrAdminUnavailable
	}
	if err != nil {
		return domain.AdminConfigVersion{}, err
	}
	return result, nil
}

func adminQueryFailure(err error) error {
	if err == nil {
		return nil
	}
	for _, known := range []error{domain.ErrUnauthenticated, domain.ErrForbidden, domain.ErrAdminNotFound, domain.ErrInvalidAdminQuery, context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, known) {
			return err
		}
	}
	return &adminQueryError{cause: err}
}

type adminQueryError struct{ cause error }

func (*adminQueryError) Error() string { return domain.MessageAdminUnavailable }
func (e *adminQueryError) Unwrap() error {
	return errors.Join(domain.ErrAdminUnavailable, e.cause)
}

var _ domain.AdminStrategies = (*adminStrategies)(nil)
