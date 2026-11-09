package venue

import (
	"context"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

// CancelConfirmed means an independent native terminal observation was recorded.
const CancelConfirmed = "CONFIRMED"

// PrepareCancel follows a fresh native active-order observation. Only cancellation
// by its immutable native ID may be retried after querying; it never places again.
func (s *Store) PrepareCancel(ctx context.Context, lease domain.LeaseToken, request CancelRequest) (CancelAdmission, error) {
	if s.matching {
		return s.prepareMatchingCancel(ctx, request)
	}
	digest, err := s.gateway.CancelDigest(request)
	account := AccountID(request.Scope.UserID, request.Scope.FundID)
	if err != nil || !validStoreContext(ctx) || lease.Key.AccountID != account || lease.Epoch <= 0 {
		return CancelAdmission{}, ErrInvalidSubmit
	}
	var row *executionmodelv1.VenueCancellation
	err = s.db.WithContext(ctx).Model(&executionmodelv1.VenueCancellation{}).Transaction(func(tx *gorm.DB) error {
		if _, _, err := lockSDKLease(ctx, tx, account, lease.OwnerID, uint64(lease.Epoch)); err != nil {
			return err
		}
		var pending []*executionmodelv1.VenueCancellation
		if err := tx.WithContext(ctx).Model(&executionmodelv1.VenueCancellation{}).
			Where("account_id = ? AND order_id = ? AND state != ?", account, strconv.FormatUint(request.OrderID, 10), CancelConfirmed).Find(&pending).Error; err != nil {
			return err
		}
		if len(pending) >= 5 {
			return domain.ErrOutcomeUnknown
		}
		for _, previous := range pending {
			if time.Now().UnixMilli()-previous.CreateAt < 500 {
				return domain.ErrOutcomeUnknown
			}
		}
		row = &executionmodelv1.VenueCancellation{
			AccountId:     account,
			UserId:        request.Scope.UserID,
			FundId:        request.Scope.FundID,
			Symbol:        request.Scope.Symbol,
			OrderId:       strconv.FormatUint(request.OrderID, 10),
			OwnerId:       lease.OwnerID,
			OwnerEpoch:    lease.Epoch,
			RequestDigest: hex.EncodeToString(digest[:]),
			State:         IntentPrepared,
		}
		return tx.WithContext(ctx).Model(&executionmodelv1.VenueCancellation{}).Create(row).Error
	})
	if err != nil {
		return CancelAdmission{}, err
	}
	return CancelAdmission{
		ID:      row.Id,
		OwnerID: lease.OwnerID,
		Epoch:   uint64(lease.Epoch),
		Request: request,
		Digest:  digest,
	}, nil
}

// WithCancellation commits the attempt before the SDK call and retains the
// execution row lock through its bounded effect. Market/strategy loss allows it.
func (s *Store) WithCancellation(ctx context.Context, admission CancelAdmission, send func(context.Context) error) error {
	if s.matching {
		return s.withMatchingCancellation(ctx, admission, send)
	}
	if !validStoreContext(ctx) || send == nil || admission.ID <= 0 {
		return ErrInvalidSubmit
	}
	account := AccountID(admission.Request.Scope.UserID, admission.Request.Scope.FundID)
	if err := s.claimCancel(ctx, account, admission); err != nil {
		return err
	}
	var callErr error
	err := s.db.WithContext(ctx).Model(&executionmodelv1.VenueCancellation{}).Transaction(func(tx *gorm.DB) error {
		lease, now, err := lockSDKLease(ctx, tx, account, admission.OwnerID, admission.Epoch)
		if err != nil {
			return err
		}
		left := time.Duration(lease.ExpiresAt-now)*time.Millisecond - 50*time.Millisecond
		if left <= 0 {
			return domain.ErrLeaseLost
		}
		bounded, cancel := context.WithTimeout(ctx, left)
		defer cancel()
		callErr = send(bounded)
		return nil
	})
	return errors.Join(err, callErr)
}

func (s *Store) claimCancel(ctx context.Context, account string, admission CancelAdmission) error {
	return s.db.WithContext(ctx).Model(&executionmodelv1.VenueCancellation{}).Transaction(func(tx *gorm.DB) error {
		if _, _, err := lockSDKLease(ctx, tx, account, admission.OwnerID, admission.Epoch); err != nil {
			return err
		}
		var row executionmodelv1.VenueCancellation
		if err := tx.WithContext(ctx).Model(&executionmodelv1.VenueCancellation{}).Where("id = ? AND delete_at IS NULL", admission.ID).
			Clauses(clause.Locking{Strength: "UPDATE"}).First(&row).Error; err != nil {
			return err
		}
		if row.State != IntentPrepared {
			return domain.ErrOutcomeUnknown
		}
		if row.AccountId != account || row.OwnerId != admission.OwnerID || row.OwnerEpoch != int64(admission.Epoch) ||
			row.RequestDigest != hex.EncodeToString(admission.Digest[:]) || row.OrderId != strconv.FormatUint(admission.Request.OrderID, 10) ||
			row.UserId != admission.Request.Scope.UserID || row.FundId != admission.Request.Scope.FundID || row.Symbol != admission.Request.Scope.Symbol {
			return ErrInvalidSubmit
		}
		return tx.WithContext(ctx).Model(&executionmodelv1.VenueCancellation{}).Where("id = ?", row.Id).Update("state", IntentDispatched).Error
	})
}
