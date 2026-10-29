package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type adminAudits struct{ snapshots adminStrategies }

// NewAdminAudits borrows storage without starting workers or writing history.
func NewAdminAudits(db *gorm.DB) (domain.AdminAudits, error) {
	if db == nil {
		return nil, domain.ErrInvalidAdminQuery
	}
	return &adminAudits{snapshots: adminStrategies{db: db}}, nil
}

func (s *adminAudits) List(ctx context.Context, token string, filter domain.AdminAuditFilter) (domain.AdminAuditPage, error) {
	if err := filter.Validate(); err != nil {
		return domain.AdminAuditPage{}, err
	}
	result := domain.AdminAuditPage{Items: make([]domain.AdminAuditEntry, 0, filter.Page.Limit)}
	err := s.snapshots.snapshot(ctx, token, func(tx *gorm.DB) error {
		query := tx.WithContext(ctx).Model(&modelv1.AdminAudit{}).
			Where("id > ? AND delete_at IS NULL", filter.Page.AfterID)
		if filter.Actor != "" {
			query = query.Where("actor = ?", filter.Actor)
		}
		if filter.Action != "" {
			query = query.Where("action = ?", filter.Action)
		}
		if filter.Resource != "" {
			query = query.Where("resource = ?", filter.Resource)
		}
		var rows []*modelv1.AdminAudit
		if err := query.Order("id ASC").Limit(filter.Page.Limit + 1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > filter.Page.Limit {
			rows = rows[:filter.Page.Limit]
			result.NextAfterID = rows[len(rows)-1].Id
		}
		for _, row := range rows {
			result.Items = append(result.Items, adminAuditEntry(row))
		}
		return nil
	})
	if err != nil {
		return domain.AdminAuditPage{}, err
	}
	return result, nil
}

func (s *adminAudits) Detail(ctx context.Context, token string, id int64) (domain.AdminAuditEntry, error) {
	if id <= 0 {
		return domain.AdminAuditEntry{}, domain.ErrInvalidAdminQuery
	}
	var result domain.AdminAuditEntry
	err := s.snapshots.snapshot(ctx, token, func(tx *gorm.DB) error {
		var row modelv1.AdminAudit
		if err := tx.WithContext(ctx).Model(&modelv1.AdminAudit{}).Where("id = ? AND delete_at IS NULL", id).Take(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrAdminNotFound
			}
			return err
		}
		result = adminAuditEntry(&row)
		return nil
	})
	if err != nil {
		return domain.AdminAuditEntry{}, err
	}
	return result, nil
}

func adminAuditEntry(row *modelv1.AdminAudit) domain.AdminAuditEntry {
	result := domain.AdminAuditEntry{
		ID:        row.Id,
		Actor:     row.Actor,
		ActorRole: domain.AdminRole(row.ActorRole),
		Action:    row.Action,
		Resource:  row.Resource,
		Reason:    row.Reason,
		CreatedAt: time.UnixMilli(row.CreateAt).UTC(),
	}
	if row.BeforeVersion != nil {
		value := *row.BeforeVersion
		result.BeforeVersion = &value
	}
	if row.AfterVersion != nil {
		value := *row.AfterVersion
		result.AfterVersion = &value
	}
	return result
}

var _ domain.AdminAudits = (*adminAudits)(nil)
