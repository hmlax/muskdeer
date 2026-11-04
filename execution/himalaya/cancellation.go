package himalaya

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"sync/atomic"
	"time"

	pb "gitlab.com/himalaya/lukla-go/pb"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

// CancelConfirmed means an independent native terminal observation was recorded.
const CancelConfirmed = "CONFIRMED"

// CancelRequest targets one known native order; it never changes fund/positions.
type CancelRequest struct {
	Scope   Scope
	OrderID uint64
}

// CancelAdmission binds the persisted cancel to the final execution owner.
type CancelAdmission struct {
	ID      int64
	OwnerID string
	Epoch   uint64
	Request CancelRequest
	Digest  [32]byte
}

// CancellationGuard grants controlled risk reduction independently of feed health.
type CancellationGuard interface {
	WithCancellation(context.Context, CancelAdmission, func(context.Context) error) error
}

func cancelInput(request CancelRequest) (*pb.CancelOrderRequest, [32]byte, error) {
	if request.Scope.UserID <= 0 || request.Scope.FundID <= 0 || request.OrderID == 0 || !validSymbol(request.Scope.Symbol) {
		return nil, [32]byte{}, ErrInvalidSubmit
	}
	input := &pb.CancelOrderRequest{
		UserId:  request.Scope.UserID,
		FundId:  request.Scope.FundID,
		Symbol:  request.Scope.Symbol,
		OrderId: request.OrderID,
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(input)
	return input, sha256.Sum256(encoded), err
}

// Cancel sends once after durable admission. Success is an acknowledgement only.
func (c *Client) Cancel(ctx context.Context, admission CancelAdmission) error {
	if c == nil || c.sdk == nil || ctx == nil {
		return ErrInvalidSubmit
	}
	guard, ok := c.guard.(CancellationGuard)
	if !ok {
		return domain.ErrLeaseLost
	}
	input, digest, err := cancelInput(admission.Request)
	if err != nil || digest != admission.Digest {
		return ErrInvalidSubmit
	}
	op, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var dispatched atomic.Bool
	err = guard.WithCancellation(op, admission, func(send context.Context) error {
		if send == nil || send.Err() != nil || op.Err() != nil {
			return ErrInvalidSubmit
		}
		deadline, bounded := send.Deadline()
		outer, _ := op.Deadline()
		if !bounded || deadline.After(outer) || !dispatched.CompareAndSwap(false, true) {
			return ErrInvalidSubmit
		}
		response, err := c.sdk.CancelOrder(send, input)
		if err != nil {
			return err
		}
		if !response.GetSuccess() {
			return domain.ErrOutcomeUnknown
		}
		return nil
	})
	if err != nil && dispatched.Load() {
		return &outcomeFailure{cause: err}
	}
	return err
}

// PrepareCancel follows a fresh native active-order observation. Only cancellation
// by its immutable native ID may be retried after querying; it never places again.
func (s *Store) PrepareCancel(ctx context.Context, lease domain.LeaseToken, request CancelRequest) (CancelAdmission, error) {
	_, digest, err := cancelInput(request)
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
