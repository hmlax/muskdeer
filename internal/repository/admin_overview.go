package repository

import (
	"context"
	"database/sql"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// Nine stored states crossed with the two requested-running values.
const maximumOverviewStateGroups = 18

type adminOverview struct{ db *gorm.DB }

// Aggregate projection only; never a persistence model or migration entity.
type adminOverviewCountRow struct {
	State          string
	DesiredRunning bool
	Count          int64
}

// NewAdminOverview borrows the existing database without starting any work.
func NewAdminOverview(db *gorm.DB) (domain.AdminOverview, error) {
	if db == nil {
		return nil, domain.ErrInvalidAdminQuery
	}
	return &adminOverview{db: db}, nil
}

func (s *adminOverview) Strategies(ctx context.Context, token string, filter domain.AdminOverviewFilter) (domain.AdminStrategyOverview, error) {
	if err := filter.Validate(); err != nil {
		return domain.AdminStrategyOverview{}, err
	}
	if err := coreContext(ctx); err != nil {
		return domain.AdminStrategyOverview{}, err
	}
	var result domain.AdminStrategyOverview
	err := s.db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
		user, _, err := readAdminAuthority(ctx, tx, token, "", domain.AdminRoleViewer, "")
		if err != nil {
			return err
		}
		now, err := adminDatabaseTime(ctx, tx, user.Id)
		if err != nil {
			return err
		}
		if now <= 0 {
			return domain.ErrAdminUnavailable
		}
		result, err = readAdminOverview(ctx, tx, filter, now)
		return err
	}, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return domain.AdminStrategyOverview{}, adminQueryFailure(err)
	}
	return result, nil
}

func adminOverviewScope(ctx context.Context, tx *gorm.DB, filter domain.AdminOverviewFilter) *gorm.DB {
	query := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("delete_at IS NULL")
	if filter.AccountID != "" {
		query = query.Where("account_id = ?", filter.AccountID)
	}
	if filter.Source != domain.MarketSourceUnspecified {
		query = query.Where("market_data_source = ?", filter.Source)
	}
	return query
}

func readAdminOverview(ctx context.Context, tx *gorm.DB, filter domain.AdminOverviewFilter, now int64) (domain.AdminStrategyOverview, error) {
	var groups []adminOverviewCountRow
	if err := adminOverviewScope(ctx, tx, filter).Select("state, desired_running, COUNT(*) AS count").
		Group("state, desired_running").Order("state ASC, desired_running ASC").Limit(maximumOverviewStateGroups + 1).Find(&groups).Error; err != nil {
		return domain.AdminStrategyOverview{}, err
	}
	result, err := adminOverviewCounts(groups)
	if err != nil {
		return domain.AdminStrategyOverview{}, err
	}
	result.ObservedAt = time.UnixMilli(now).UTC()
	var rows []*modelv1.StrategyInstance
	if err := adminOverviewScope(ctx, tx, filter).Where("id > ?", filter.Page.AfterID).
		Order("id ASC").Limit(filter.Page.Limit + 1).Find(&rows).Error; err != nil {
		return domain.AdminStrategyOverview{}, err
	}
	hasMore := len(rows) > filter.Page.Limit
	if hasMore {
		rows = rows[:filter.Page.Limit]
	}
	leases, err := readAdminOverviewLeases(ctx, tx, rows, now)
	if err != nil {
		return domain.AdminStrategyOverview{}, err
	}
	result.Items = make([]domain.AdminStrategyRuntime, 0, len(rows))
	for _, row := range rows {
		if row == nil || row.Id <= 0 || !domain.ValidStrategyState(domain.StrategyState(row.State)) {
			return domain.AdminStrategyOverview{}, domain.ErrAdminUnavailable
		}
		result.Items = append(result.Items, domain.AdminStrategyRuntime{
			Strategy: adminStrategy(row),
			Lease:    leases[row.Id],
		})
	}
	if hasMore {
		result.NextAfterID = result.Items[len(result.Items)-1].Strategy.Spec.ID
	}
	return result, nil
}

func adminOverviewCounts(rows []adminOverviewCountRow) (domain.AdminStrategyOverview, error) {
	result := domain.AdminStrategyOverview{StoredStates: make([]domain.AdminStoredStateCount, 0)}
	if len(rows) > maximumOverviewStateGroups {
		return domain.AdminStrategyOverview{}, domain.ErrAdminUnavailable
	}
	for i, row := range rows {
		state := domain.StrategyState(row.State)
		if !domain.ValidStrategyState(state) || row.Count <= 0 || result.Total > math.MaxInt64-row.Count ||
			i > 0 && (row.State < rows[i-1].State || row.State == rows[i-1].State && (rows[i-1].DesiredRunning || !row.DesiredRunning)) {
			return domain.AdminStrategyOverview{}, domain.ErrAdminUnavailable
		}
		result.Total += row.Count
		if row.DesiredRunning {
			result.RequestedRunning += row.Count
		}
		last := len(result.StoredStates) - 1
		if last >= 0 && result.StoredStates[last].State == state {
			result.StoredStates[last].Count += row.Count
		} else {
			result.StoredStates = append(result.StoredStates, domain.AdminStoredStateCount{
				State: state,
				Count: row.Count,
			})
		}
	}
	return result, nil
}

func readAdminOverviewLeases(ctx context.Context, tx *gorm.DB, rows []*modelv1.StrategyInstance, now int64) (map[int64]*domain.AdminLeaseObservation, error) {
	result := make(map[int64]*domain.AdminLeaseObservation, len(rows))
	if len(rows) == 0 {
		return result, nil
	}
	ids := make([]int64, 0, len(rows))
	strategies := make(map[int64]*modelv1.StrategyInstance, len(rows))
	for _, row := range rows {
		if row == nil || row.Id <= 0 {
			return nil, domain.ErrAdminUnavailable
		}
		ids = append(ids, row.Id)
		strategies[row.Id] = row
	}
	var leases []*modelv1.StrategyLease
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyLease{}).
		Where("strategy_instance_id IN ? AND delete_at IS NULL", ids).Find(&leases).Error; err != nil {
		return nil, err
	}
	for _, row := range leases {
		if row == nil {
			return nil, domain.ErrAdminUnavailable
		}
		strategy := strategies[row.StrategyInstanceId]
		if strategy == nil || result[row.StrategyInstanceId] != nil {
			return nil, domain.ErrAdminUnavailable
		}
		value, err := adminOverviewLeaseValue(row, strategy, now)
		if err != nil {
			return nil, err
		}
		result[row.StrategyInstanceId] = value
	}
	return result, nil
}

func adminOverviewLeaseValue(row *modelv1.StrategyLease, strategy *modelv1.StrategyInstance, now int64) (*domain.AdminLeaseObservation, error) {
	if row == nil || strategy == nil || row.Id <= 0 || strategy.Id <= 0 || row.StrategyInstanceId != strategy.Id || now <= 0 ||
		row.Epoch < 0 || row.ExpiresAt < 0 || len(row.OwnerId) > 512 || !utf8.ValidString(row.OwnerId) ||
		utf8.RuneCountInString(row.OwnerId) > 128 || strings.ContainsRune(row.OwnerId, 0) {
		return nil, domain.ErrAdminUnavailable
	}
	ownerRecorded := row.OwnerId != ""
	if row.Epoch == 0 && (ownerRecorded || row.ExpiresAt != 0) || row.Epoch > 0 && !ownerRecorded {
		return nil, domain.ErrAdminUnavailable
	}
	return &domain.AdminLeaseObservation{
		Epoch:                  row.Epoch,
		OwnerRecorded:          ownerRecorded,
		ExpiresAt:              time.UnixMilli(row.ExpiresAt).UTC(),
		UnexpiredAtObservation: ownerRecorded && row.ExpiresAt > now,
		RuntimeMatchesLease:    ownerRecorded && row.OwnerId == strategy.RuntimeOwnerId && row.Epoch == strategy.RuntimeOwnerEpoch,
	}, nil
}

var _ domain.AdminOverview = (*adminOverview)(nil)
