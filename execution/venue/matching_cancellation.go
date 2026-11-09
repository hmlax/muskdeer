package venue

import (
	"context"
	"encoding/hex"
	"strconv"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	execution "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func (s *Store) prepareMatchingCancel(ctx context.Context, request CancelRequest) (CancelAdmission, error) {
	if !validStoreContext(ctx) {
		return CancelAdmission{}, ErrInvalidSubmit
	}
	digest, err := s.gateway.CancelDigest(request)
	if err != nil {
		return CancelAdmission{}, err
	}
	row := &execution.VenueCancellation{
		AccountId:     AccountID(request.Scope.UserID, request.Scope.FundID),
		UserId:        request.Scope.UserID,
		FundId:        request.Scope.FundID,
		Symbol:        request.Scope.Symbol,
		OrderId:       strconv.FormatUint(request.OrderID, 10),
		RequestDigest: hex.EncodeToString(digest[:]),
		State:         IntentPrepared,
	}
	if err := s.db.WithContext(ctx).Model(&execution.VenueCancellation{}).Create(row).Error; err != nil {
		return CancelAdmission{}, err
	}
	return CancelAdmission{
		ID:      row.Id,
		Request: request,
		Digest:  digest,
	}, nil
}

func (s *Store) withMatchingCancellation(ctx context.Context, admission CancelAdmission, send func(context.Context) error) error {
	if !validStoreContext(ctx) || send == nil || admission.ID <= 0 || admission.OwnerID != "" || admission.Epoch != 0 {
		return ErrInvalidSubmit
	}
	err := s.db.WithContext(ctx).Model(&execution.VenueCancellation{}).Transaction(func(tx *gorm.DB) error {
		var row execution.VenueCancellation
		if err := tx.WithContext(ctx).Model(&execution.VenueCancellation{}).Where("id = ? AND delete_at IS NULL", admission.ID).
			Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row).Error; err != nil {
			return err
		}
		if row.State != IntentPrepared {
			return domain.ErrOutcomeUnknown
		}
		if row.OwnerId != "" || row.OwnerEpoch != 0 || row.AccountId != AccountID(admission.Request.Scope.UserID, admission.Request.Scope.FundID) ||
			row.RequestDigest != hex.EncodeToString(admission.Digest[:]) || row.OrderId != strconv.FormatUint(admission.Request.OrderID, 10) ||
			row.UserId != admission.Request.Scope.UserID || row.FundId != admission.Request.Scope.FundID || row.Symbol != admission.Request.Scope.Symbol {
			return ErrInvalidSubmit
		}
		return tx.WithContext(ctx).Model(&execution.VenueCancellation{}).Where("id = ?", row.Id).Update("state", IntentDispatched).Error
	})
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, maximumOperationTimeout)
	defer cancel()
	return send(bounded)
}
