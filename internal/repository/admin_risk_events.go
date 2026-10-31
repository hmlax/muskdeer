package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type adminRiskEvents struct{ snapshots adminStrategies }

// NewAdminRiskEvents borrows storage without evaluating or refreshing risk.
func NewAdminRiskEvents(db *gorm.DB) (domain.AdminRiskEvents, error) {
	if db == nil {
		return nil, domain.ErrInvalidAdminQuery
	}
	return &adminRiskEvents{snapshots: adminStrategies{db: db}}, nil
}

func (s *adminRiskEvents) List(ctx context.Context, token string, filter domain.AdminRiskEventFilter) (domain.AdminRiskEventPage, error) {
	if err := filter.Validate(); err != nil {
		return domain.AdminRiskEventPage{}, err
	}
	var result domain.AdminRiskEventPage
	err := s.snapshots.snapshot(ctx, token, func(tx *gorm.DB) error {
		query := adminRiskEventQuery(ctx, tx, filter)
		var rows []*modelv1.RiskSnapshotEvent
		if err := query.Order("id ASC").Limit(filter.Page.Limit + 1).Find(&rows).Error; err != nil {
			return err
		}
		hasMore := len(rows) > filter.Page.Limit
		if hasMore {
			rows = rows[:filter.Page.Limit]
		}
		result.Items = make([]domain.AdminRiskEvent, 0, len(rows))
		for _, row := range rows {
			value, err := adminRiskEventValue(row)
			if err != nil {
				return err
			}
			result.Items = append(result.Items, value)
		}
		if hasMore {
			result.NextAfterID = result.Items[len(result.Items)-1].ID
		}
		return nil
	})
	if err != nil {
		return domain.AdminRiskEventPage{}, err
	}
	return result, nil
}

func (s *adminRiskEvents) Detail(ctx context.Context, token string, id int64) (domain.AdminRiskEvent, error) {
	if id <= 0 {
		return domain.AdminRiskEvent{}, domain.ErrInvalidAdminQuery
	}
	var result domain.AdminRiskEvent
	err := s.snapshots.snapshot(ctx, token, func(tx *gorm.DB) error {
		var row modelv1.RiskSnapshotEvent
		if err := tx.WithContext(ctx).Model(&modelv1.RiskSnapshotEvent{}).
			Where("id = ? AND delete_at IS NULL", id).Take(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrAdminNotFound
			}
			return err
		}
		var err error
		result, err = adminRiskEventValue(&row)
		return err
	})
	if err != nil {
		return domain.AdminRiskEvent{}, err
	}
	return result, nil
}

func adminRiskEventQuery(ctx context.Context, tx *gorm.DB, filter domain.AdminRiskEventFilter) *gorm.DB {
	query := tx.WithContext(ctx).Model(&modelv1.RiskSnapshotEvent{}).
		Where("id > ? AND delete_at IS NULL", filter.Page.AfterID)
	if filter.StrategyID != 0 {
		query = query.Where("strategy_instance_id = ?", filter.StrategyID)
	}
	if filter.AccountID != "" {
		query = query.Where("account_id = ?", filter.AccountID)
	}
	if filter.InstrumentID != 0 {
		query = query.Where("instrument_id = ?", filter.InstrumentID)
	}
	if filter.Source != domain.MarketSourceUnspecified {
		query = query.Where("market_data_source = ?", filter.Source)
	}
	if filter.Kind != domain.RiskSnapshotEventUnspecified {
		query = query.Where("kind = ?", filter.Kind)
	}
	if filter.Reason != domain.RiskReasonUnspecified {
		query = query.Where("reason_code = ?", filter.Reason)
	}
	return query
}

func adminRiskEventValue(row *modelv1.RiskSnapshotEvent) (domain.AdminRiskEvent, error) {
	if !validAdminRiskEventIdentity(row) || !validAdminRiskEventObservation(row) {
		return domain.AdminRiskEvent{}, domain.ErrAdminUnavailable
	}
	result := domain.AdminRiskEvent{
		ID:                   row.Id,
		RecordedAt:           time.UnixMilli(row.CreateAt).UTC(),
		Kind:                 domain.RiskSnapshotEventKind(row.Kind),
		SnapshotID:           row.RiskSnapshotId,
		SnapshotRevision:     row.RiskSnapshotRevision,
		StrategyID:           row.StrategyInstanceId,
		AccountID:            row.AccountId,
		InstrumentID:         row.InstrumentId,
		ConfigVersion:        row.ConfigVersion,
		Source:               domain.MarketSource(row.MarketDataSource),
		PublisherEpoch:       row.MarketEpoch,
		MarketSequence:       row.MarketSequence,
		PublicationRevision:  row.MarketRevision,
		SourceVersion:        row.MarketSourceVersion,
		MarketMaximumAge:     time.Duration(row.MarketMaximumAgeMs) * time.Millisecond,
		ObservedAt:           time.UnixMilli(row.ObservedAt).UTC(),
		ExpiresAt:            time.UnixMilli(row.ExpiresAt).UTC(),
		RecordedReconciled:   row.Reconciled,
		RecordedAllowNewRisk: row.AllowNewRisk,
		Reason:               domain.RiskReason(row.ReasonCode),
		SnapshotDeleted:      row.SnapshotDeleted,
	}
	if row.MarketFeedId != nil {
		id := *row.MarketFeedId
		result.FeedID = &id
	}
	return result, nil
}

func validAdminRiskEventIdentity(row *modelv1.RiskSnapshotEvent) bool {
	return row != nil && row.Id > 0 && row.CreateAt > 0 && row.UpdateAt == row.CreateAt && row.DeleteAt == nil &&
		row.RiskSnapshotId > 0 && row.RiskSnapshotRevision > 0 && row.StrategyInstanceId > 0 &&
		coreIdentifier(row.AccountId, 64) && row.InstrumentId > 0 && row.ConfigVersion > 0 &&
		domain.ValidRiskSnapshotEventKind(domain.RiskSnapshotEventKind(row.Kind)) &&
		(row.Kind != string(domain.RiskSnapshotEventRemoved) || row.SnapshotDeleted)
}

func validAdminRiskEventObservation(row *modelv1.RiskSnapshotEvent) bool {
	return domain.MarketSource(row.MarketDataSource).ValidateSelection() == nil &&
		(row.MarketFeedId == nil || *row.MarketFeedId > 0) && row.MarketEpoch >= 0 && row.MarketSequence >= 0 &&
		row.MarketRevision >= 0 && row.MarketSourceVersion >= 0 && row.MarketMaximumAgeMs > 0 &&
		row.MarketMaximumAgeMs <= domain.MaxRiskSnapshotAge.Milliseconds() && row.ObservedAt > 0 &&
		row.ExpiresAt > row.ObservedAt && row.ExpiresAt-row.ObservedAt <= domain.MaxRiskSnapshotAge.Milliseconds() &&
		domain.ValidRiskReason(domain.RiskReason(row.ReasonCode))
}

var _ domain.AdminRiskEvents = (*adminRiskEvents)(nil)
