package data

import (
	"context"
	"errors"
	"time"

	"github.com/tx7do/go-crud/viewer"
	"github.com/tx7do/go-utils/trans"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/hmlax/muskdeer/deer/management/muskdeer"
	pb "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data/store"
)

const (
	muskdeerBindingPendingVersion uint64 = 0
	muskdeerBindingActiveVersion  uint64 = 1
	muskdeerUserScopeDenied              = "trading user is not exclusively authorized for this merchant"
)

// The existing module directory row serializes low-frequency enrollments,
// including platform grants. No database lock is held over a core RPC.
func lockMuskdeerEnrollment(ctx context.Context, db *gorm.DB) error {
	var group store.PermissionGroup
	return db.WithContext(ctx).Model(&store.PermissionGroup{}).Where("module = ?", muskdeer.Module).
		Order("id").Clauses(clause.Locking{Strength: "UPDATE"}).Take(&group).Error
}

func userBindings(ctx context.Context, db *gorm.DB, user int64) ([]*store.MuskdeerAccountBinding, error) {
	var rows []*store.MuskdeerAccountBinding
	err := db.WithContext(ctx).Model(&store.MuskdeerAccountBinding{}).
		Where("account_id LIKE ?", muskdeer.UserScopePrefix(user)+"%").Order("id").Find(&rows).Error
	return rows, err
}

func exclusiveMuskdeerUser(rows []*store.MuskdeerAccountBinding, tenant uint32, requireActive bool) error {
	active := false
	for _, row := range rows {
		if valueOrZero(row.TenantID) != tenant {
			return status.Error(codes.PermissionDenied, muskdeerUserScopeDenied)
		}
		active = active || (row.DeletedAt == nil && valueOrZero(row.IsEnabled) && valueOrZero(row.Version) > muskdeerBindingPendingVersion)
	}
	if requireActive && !active {
		return status.Error(codes.PermissionDenied, muskdeerUserScopeDenied)
	}
	return nil
}

// BeginAccountEnrollment records permanent tenant ownership before core registration.
// Version zero is an inactive, unfinished enrollment, never a usable binding.
func (r *MuskdeerBindingRepo) BeginAccountEnrollment(ctx context.Context, user, fund int64) (*pb.MuskdeerAccountBinding, error) {
	actor, err := MuskdeerIdentity(ctx, false)
	if err != nil || actor.GetTenantId() == 0 {
		return nil, status.Error(codes.PermissionDenied, muskdeerUserScopeDenied)
	}
	if user <= 0 || fund <= 0 {
		return nil, status.Error(codes.InvalidArgument, muskdeerUserScopeDenied)
	}
	var result *store.MuskdeerAccountBinding
	trusted := viewer.WithSystemContext(ctx)
	err = r.db.Client().GormDB().WithContext(trusted).Model(&store.MuskdeerAccountBinding{}).Transaction(func(tx *gorm.DB) error {
		if err := lockMuskdeerEnrollment(trusted, tx); err != nil {
			return err
		}
		rows, err := userBindings(trusted, tx, user)
		if err != nil {
			return err
		}
		if err := exclusiveMuskdeerUser(rows, actor.GetTenantId(), true); err != nil {
			return err
		}
		account := muskdeer.AccountScope(user, fund)
		for _, row := range rows {
			if valueOrZero(row.AccountID) != account {
				continue
			}
			if row.DeletedAt != nil || valueOrZero(row.Version) != muskdeerBindingPendingVersion || valueOrZero(row.IsEnabled) {
				return status.Error(codes.AlreadyExists, muskdeerUserScopeDenied)
			}
			result = row
			return nil
		}
		now := time.Now().UTC()
		result = &store.MuskdeerAccountBinding{
			TenantID:  trans.Ptr(actor.GetTenantId()),
			AccountID: trans.Ptr(account),
			IsEnabled: trans.Ptr(false),
			Version:   trans.Ptr(muskdeerBindingPendingVersion),
			CreatedAt: &now,
			UpdatedAt: &now,
			CreatedBy: trans.Ptr(actor.GetUserId()),
			UpdatedBy: trans.Ptr(actor.GetUserId()),
		}
		return tx.WithContext(trusted).Model(&store.MuskdeerAccountBinding{}).Create(result).Error
	})
	if err != nil {
		return nil, err
	}
	return bindingDTO(result), nil
}

// CompleteAccountEnrollment activates only a successful enrollment. A later revocation or
// normal disabled binding cannot be re-enabled through an add-account request.
func (r *MuskdeerBindingRepo) CompleteAccountEnrollment(ctx context.Context, id uint32) error {
	actor, err := MuskdeerIdentity(ctx, false)
	if err != nil || actor.GetTenantId() == 0 {
		return status.Error(codes.PermissionDenied, muskdeerUserScopeDenied)
	}
	return r.db.Client().GormDB().WithContext(ctx).Model(&store.MuskdeerAccountBinding{}).Transaction(func(tx *gorm.DB) error {
		var row store.MuskdeerAccountBinding
		if err := tx.WithContext(ctx).Model(&store.MuskdeerAccountBinding{}).
			Where("id = ? AND tenant_id = ? AND deleted_at IS NULL", id, actor.GetTenantId()).
			Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row).Error; err != nil {
			return err
		}
		if valueOrZero(row.Version) == muskdeerBindingActiveVersion && valueOrZero(row.IsEnabled) {
			return nil
		}
		if valueOrZero(row.Version) != muskdeerBindingPendingVersion || valueOrZero(row.IsEnabled) {
			return status.Error(codes.Aborted, muskdeerUserScopeDenied)
		}
		row.IsEnabled = trans.Ptr(true)
		row.Version = trans.Ptr(muskdeerBindingActiveVersion)
		row.UpdatedBy = trans.Ptr(actor.GetUserId())
		now := time.Now().UTC()
		row.UpdatedAt = &now
		update := tx.WithContext(ctx).Model(&store.MuskdeerAccountBinding{}).
			Where("id = ? AND tenant_id = ? AND version = ? AND is_enabled = ?", id, actor.GetTenantId(), muskdeerBindingPendingVersion, false).
			Select("IsEnabled", "Version", "UpdatedAt", "UpdatedBy").Updates(&row)
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return errors.New(muskdeerUserScopeDenied)
		}
		return nil
	})
}
