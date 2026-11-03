package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/hmlax/muskdeer/deer/ownerid"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

// CheckNativeAuthority validates a read publication; it does not authorize effects.
func CheckNativeAuthority(ctx context.Context, db *gorm.DB, authority *commonv1.HimalayaAuthority) error {
	if !validNativeAuthority(ctx, authority) || db == nil {
		return domain.ErrLeaseLost
	}
	var count int64
	err := db.WithContext(ctx).Model(&modelv1.ExecutionShardLease{}).
		Where("account_id = ? AND owner_id = ? AND epoch = ? AND delete_at IS NULL", authority.AccountId, authority.OwnerId, authority.Epoch).
		Where("expires_at > floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint").Count(&count).Error
	if err != nil {
		return err
	}
	if count != 1 {
		return domain.ErrLeaseLost
	}
	return nil
}

// LockNativeAuthority requires a caller transaction. The retained row lock is
// held through the caller's effect; expiry is checked using database time.
func LockNativeAuthority(ctx context.Context, tx *gorm.DB, authority *commonv1.HimalayaAuthority) (time.Duration, error) {
	if !validNativeAuthority(ctx, authority) || tx == nil {
		return 0, domain.ErrLeaseLost
	}
	var row modelv1.ExecutionShardLease
	if err := tx.WithContext(ctx).Model(&modelv1.ExecutionShardLease{}).Where("account_id = ? AND delete_at IS NULL", authority.AccountId).
		Clauses(clause.Locking{Strength: "UPDATE"}).First(&row).Error; err != nil {
		return 0, err
	}
	var clock struct{ NowMS int64 }
	if err := tx.WithContext(ctx).Model(&modelv1.ExecutionShardLease{}).Where("id = ?", row.Id).
		Select("floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint AS now_ms").Take(&clock).Error; err != nil {
		return 0, err
	}
	until := row.ExpiresAt
	if authority.ExpiresAtMs > 0 {
		until = min(until, authority.ExpiresAtMs)
	}
	remaining := time.Duration(until-clock.NowMS)*time.Millisecond - 50*time.Millisecond
	if row.OwnerId != authority.OwnerId || row.Epoch != authority.Epoch || remaining <= 0 {
		return 0, domain.ErrLeaseLost
	}
	return remaining, nil
}

func validNativeAuthority(ctx context.Context, authority *commonv1.HimalayaAuthority) bool {
	return ctx != nil && ctx.Err() == nil && authority.GetEpoch() > 0 && authority.GetAccountId() != "" &&
		len(authority.AccountId) <= 64 && ownerid.Valid(authority.GetOwnerId())
}

// NativeExecutionInstance resolves exactly the retained fund execution owner.
func NativeExecutionInstance(ctx context.Context, db *gorm.DB, account string) (string, error) {
	var row modelv1.ExecutionShardLease
	if err := db.WithContext(ctx).Model(&modelv1.ExecutionShardLease{}).
		Where("account_id = ? AND delete_at IS NULL AND epoch > 0", account).
		Where("expires_at > floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint").First(&row).Error; err != nil {
		return "", err
	}
	instance, valid := ownerid.Instance(row.OwnerId)
	if !valid {
		return "", domain.ErrLeaseLost
	}
	return instance, nil
}
