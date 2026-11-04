package himalaya

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"strconv"
	"time"

	"github.com/shopspring/decimal"
	pb "gitlab.com/himalaya/lukla-go/pb"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/hmlax/muskdeer/deer/ownerid"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

// Durable SDK intent states. DISPATCHED is ambiguous until native evidence.
const (
	IntentPrepared     = "PREPARED"
	IntentDispatched   = "DISPATCHED"
	IntentAcknowledged = "ACKNOWLEDGED"
	IntentTerminal     = "TERMINAL"
	messageRiskLimit   = "himalaya SDK risk admission rejected"
)

// ErrRiskLimit rejects stale snapshots, disabled accounts and exhausted budgets.
var ErrRiskLimit = errors.New(messageRiskLimit)

// Store borrows the process database; it does not open pools or start tasks.
type Store struct{ db *gorm.DB }

// Prepared binds the immutable SDK request to its durable admission identity.
type Prepared struct {
	Request   SubmitRequest
	Admission Admission
}

// NewStore constructs the final Muskdeer SDK admission boundary.
func NewStore(db *gorm.DB) (*Store, error) {
	if db == nil {
		return nil, ErrInvalidConfig
	}
	return &Store{db: db}, nil
}

// AccountID gives all symbols in one native fund the same execution/risk scope.
func AccountID(userID, fundID int64) string {
	return "himalaya/" + strconv.FormatInt(userID, 10) + "/" + strconv.FormatInt(fundID, 10)
}

// Prepare reserves gross notional and persists the exact intent atomically.
// It does not dispatch or authorize a later call without another epoch check.
func (s *Store) Prepare(ctx context.Context, lease domain.LeaseToken, request SubmitRequest) (Prepared, error) {
	return s.prepare(ctx, lease, request, nil)
}

func (s *Store) prepare(ctx context.Context, lease domain.LeaseToken, request SubmitRequest, live *LiveAuthority) (Prepared, error) {
	if s == nil || s.db == nil {
		return Prepared{}, ErrInvalidConfig
	}
	request.IntentID = 1 // A positive provisional ID is excluded from the SDK wire digest.
	request.OwnerID, request.Epoch = lease.OwnerID, uint64(lease.Epoch)
	digest, err := requestDigest(request)
	if err != nil || !validStoreContext(ctx) || lease.Epoch <= 0 || lease.Key.Kind != domain.LeaseKindExecution ||
		lease.Key.AccountID != AccountID(request.UserID, request.FundID) {
		return Prepared{}, ErrInvalidSubmit
	}
	var row *modelv1.HimalayaIntent
	err = s.db.WithContext(ctx).Model(&modelv1.HimalayaIntent{}).Transaction(func(tx *gorm.DB) error {
		_, now, err := lockSDKLease(ctx, tx, lease.Key.AccountID, lease.OwnerID, uint64(lease.Epoch))
		if err != nil {
			return err
		}
		budget, err := lockSDKBudget(ctx, tx, lease.Key.AccountID)
		if err != nil {
			return err
		}
		if !budget.Enabled || budget.SnapshotExpiresAt <= now || budget.UserId != request.UserID || budget.FundId != request.FundID {
			return ErrRiskLimit
		}
		var ambiguous int64
		if err := tx.WithContext(ctx).Model(&modelv1.HimalayaIntent{}).
			Where("account_id = ? AND state = ? AND historical_approval_reference = ''", lease.Key.AccountID, IntentDispatched).Count(&ambiguous).Error; err != nil {
			return err
		}
		if ambiguous != 0 {
			return domain.ErrOutcomeUnknown
		}
		maximum, observed, reserved, err := budgetAmounts(budget)
		if err != nil {
			return err
		}
		notional := request.Price.Mul(request.Quantity)
		if observed.Add(reserved).Add(notional).GreaterThan(maximum) {
			return ErrRiskLimit
		}
		row = &modelv1.HimalayaIntent{
			AccountId:        lease.Key.AccountID,
			UserId:           request.UserID,
			FundId:           request.FundID,
			Symbol:           request.Symbol,
			ClientId:         request.ClientID,
			OwnerId:          request.OwnerID,
			OwnerEpoch:       int64(request.Epoch),
			RequestDigest:    hex.EncodeToString(digest[:]),
			Side:             string(request.Side),
			Price:            request.Price.String(),
			Quantity:         request.Quantity.String(),
			Leverage:         request.Leverage,
			State:            IntentPrepared,
			ReservedNotional: notional.String(),
		}
		if live != nil {
			if err := checkLiveBudget(budget, request, reserved.Add(notional)); err != nil {
				return err
			}
			setLiveAuthority(ctx, row, live, now)
			if _, err := checkLiveAuthority(ctx, tx, row, now); err != nil {
				return err
			}
		}
		if err := tx.WithContext(ctx).Model(&modelv1.HimalayaIntent{}).Create(row).Error; err != nil {
			return err
		}
		return tx.WithContext(ctx).Model(&modelv1.HimalayaBudget{}).Where("id = ?", budget.Id).
			Update("reserved_notional", reserved.Add(notional).String()).Error
	})
	if err != nil {
		return Prepared{}, err
	}
	request.IntentID = row.Id
	return Prepared{
		Request: request,
		Admission: Admission{
			IntentID: row.Id,
			OwnerID:  request.OwnerID,
			Epoch:    request.Epoch,
			UserID:   request.UserID,
			FundID:   request.FundID,
			ClientID: request.ClientID,
			Digest:   digest,
		},
	}, nil
}

// WithSubmission commits a one-way claim before the effect, then locks and
// rechecks the current lease at SDK egress. Lease rows remain locked through
// the bounded callback. A crash leaves DISPATCHED, which blocks new risk.
// This cannot revoke a packet already accepted by the unchanged native server.
func (s *Store) WithSubmission(ctx context.Context, admission Admission, send func(context.Context) error) error {
	if s == nil || s.db == nil || !validStoreContext(ctx) || send == nil || admission.IntentID <= 0 {
		return ErrInvalidSubmit
	}
	account := AccountID(admission.UserID, admission.FundID)
	if err := s.claim(ctx, admission, account); err != nil {
		return err
	}
	var callErr error
	called := false
	err := s.db.WithContext(ctx).Model(&modelv1.HimalayaIntent{}).Transaction(func(tx *gorm.DB) error {
		lease, now, err := lockSDKLease(ctx, tx, account, admission.OwnerID, admission.Epoch)
		if err != nil {
			return err
		}
		remaining := time.Duration(lease.ExpiresAt-now)*time.Millisecond - 50*time.Millisecond
		if remaining <= 0 {
			return domain.ErrLeaseLost
		}
		budget, err := lockSDKBudget(ctx, tx, account)
		if err != nil {
			return err
		}
		if !budget.Enabled || budget.SnapshotExpiresAt <= now {
			return ErrRiskLimit
		}
		if err := checkReservedBudget(budget); err != nil {
			return err
		}
		if err := checkDevelopmentRiskHold(ctx, tx, budget); err != nil {
			return err
		}
		freshness := time.Duration(budget.SnapshotExpiresAt-now)*time.Millisecond - 50*time.Millisecond
		if freshness <= 0 {
			return ErrRiskLimit
		}
		remaining = min(remaining, freshness)
		var row modelv1.HimalayaIntent
		if err := tx.WithContext(ctx).Model(&modelv1.HimalayaIntent{}).
			Where("id = ? AND state = ? AND delete_at IS NULL", admission.IntentID, IntentDispatched).
			Clauses(clause.Locking{Strength: "UPDATE"}).First(&row).Error; err != nil {
			return err
		}
		if !matchesAdmission(&row, admission) {
			return ErrInvalidSubmit
		}
		if err := checkPersistedLiveBudget(budget, &row); err != nil {
			return err
		}
		left, err := checkLiveAuthority(ctx, tx, &row, now)
		if err != nil {
			return err
		}
		remaining = min(remaining, left)
		remaining, err = liveSubmissionWindow(ctx, &row, remaining)
		if err != nil {
			return err
		}
		bounded, cancel := context.WithTimeout(ctx, remaining)
		defer cancel()
		called = true
		callErr = send(bounded)
		return nil // Preserve the committed claim even when the SDK reply is lost.
	})
	if !called || errors.Is(callErr, errSDKNotDispatched) {
		return errors.Join(err, callErr, s.abandonUnsent(ctx, admission))
	}
	return errors.Join(err, callErr)
}

// Acknowledge records a known native identity without releasing any risk. The
// caller must still reconcile orders, fills and positions before releasing it.
// Missing/failed acknowledgement leaves DISPATCHED and cannot trigger a resend.
func (s *Store) Acknowledge(ctx context.Context, admission Admission, response *pb.PlaceOrderResponse) error {
	if s == nil || s.db == nil || !validStoreContext(ctx) || response.GetOrderId() == 0 || response.GetClientId() != admission.ClientID {
		return ErrInvalidSubmit
	}
	return s.db.WithContext(ctx).Model(&modelv1.HimalayaIntent{}).Transaction(func(tx *gorm.DB) error {
		account := AccountID(admission.UserID, admission.FundID)
		if _, _, err := lockSDKLease(ctx, tx, account, admission.OwnerID, admission.Epoch); err != nil {
			return err
		}
		var row modelv1.HimalayaIntent
		if err := tx.WithContext(ctx).Model(&modelv1.HimalayaIntent{}).Where("id = ? AND delete_at IS NULL", admission.IntentID).
			Clauses(clause.Locking{Strength: "UPDATE"}).First(&row).Error; err != nil {
			return err
		}
		if !matchesAdmission(&row, admission) {
			return ErrInvalidSubmit
		}
		orderID := strconv.FormatUint(response.OrderId, 10)
		if row.State == IntentAcknowledged && row.OrderId == orderID {
			return nil
		}
		if row.State != IntentDispatched {
			return domain.ErrOutcomeUnknown
		}
		row.State, row.OrderId = IntentAcknowledged, orderID
		return tx.WithContext(ctx).Model(&modelv1.HimalayaIntent{}).Where("id = ?", row.Id).
			Select("State", "OrderId").Updates(&row).Error
	})
}

func (s *Store) claim(ctx context.Context, admission Admission, account string) error {
	return s.db.WithContext(ctx).Model(&modelv1.HimalayaIntent{}).Transaction(func(tx *gorm.DB) error {
		_, now, err := lockSDKLease(ctx, tx, account, admission.OwnerID, admission.Epoch)
		if err != nil {
			return err
		}
		budget, err := lockSDKBudget(ctx, tx, account)
		if err != nil {
			return err
		}
		if !budget.Enabled || budget.SnapshotExpiresAt <= now {
			return ErrRiskLimit
		}
		if err := checkReservedBudget(budget); err != nil {
			return err
		}
		var row modelv1.HimalayaIntent
		if err := tx.WithContext(ctx).Model(&modelv1.HimalayaIntent{}).Where("id = ? AND delete_at IS NULL", admission.IntentID).
			Clauses(clause.Locking{Strength: "UPDATE"}).First(&row).Error; err != nil {
			return err
		}
		if row.State != IntentPrepared {
			return domain.ErrOutcomeUnknown
		}
		if !matchesAdmission(&row, admission) {
			return ErrInvalidSubmit
		}
		left, err := checkLiveAuthority(ctx, tx, &row, now)
		if err != nil {
			return err
		}
		if _, err := liveSubmissionWindow(ctx, &row, left); err != nil {
			return err
		}
		if err := checkPersistedLiveBudget(budget, &row); err != nil {
			return err
		}
		var pending int64
		if err := tx.WithContext(ctx).Model(&modelv1.HimalayaIntent{}).
			Where("account_id = ? AND state = ? AND historical_approval_reference = ''", account, IntentDispatched).Count(&pending).Error; err != nil {
			return err
		}
		if pending > 0 {
			return domain.ErrOutcomeUnknown
		}
		cancelQuery, err := pendingSDKCancellations(ctx, tx, account)
		if err != nil {
			return err
		}
		if err := cancelQuery.Where("state = ?", IntentDispatched).Count(&pending).Error; err != nil {
			return err
		}
		if pending > 0 {
			return domain.ErrOutcomeUnknown
		}
		return tx.WithContext(ctx).Model(&modelv1.HimalayaIntent{}).Where("id = ?", row.Id).
			Update("state", IntentDispatched).Error
	})
}

func matchesAdmission(row *modelv1.HimalayaIntent, admission Admission) bool {
	if row.AccountId != AccountID(admission.UserID, admission.FundID) || row.UserId != admission.UserID || row.FundId != admission.FundID ||
		row.ClientId != admission.ClientID || row.OwnerId != admission.OwnerID || row.OwnerEpoch != int64(admission.Epoch) ||
		row.RequestDigest != hex.EncodeToString(admission.Digest[:]) {
		return false
	}
	price, e1 := decimal.NewFromString(row.Price)
	quantity, e2 := decimal.NewFromString(row.Quantity)
	if e1 != nil || e2 != nil {
		return false
	}
	digest, err := requestDigest(SubmitRequest{
		IntentID: row.Id,
		OwnerID:  row.OwnerId,
		Epoch:    uint64(row.OwnerEpoch),
		UserID:   row.UserId,
		FundID:   row.FundId,
		Symbol:   row.Symbol,
		ClientID: row.ClientId,
		Side:     domain.Side(row.Side),
		Price:    price,
		Quantity: quantity,
		Leverage: row.Leverage,
	})
	return err == nil && digest == admission.Digest
}

func lockSDKLease(ctx context.Context, tx *gorm.DB, account, owner string, epoch uint64) (*modelv1.ExecutionShardLease, int64, error) {
	if epoch == 0 || epoch > math.MaxInt64 || !ownerid.Valid(owner) {
		return nil, 0, domain.ErrLeaseLost
	}
	var row modelv1.ExecutionShardLease
	if err := tx.WithContext(ctx).Model(&modelv1.ExecutionShardLease{}).Where("account_id = ? AND delete_at IS NULL", account).
		Clauses(clause.Locking{Strength: "UPDATE"}).First(&row).Error; err != nil {
		return nil, 0, err
	}
	var clock struct{ NowMS int64 }
	if err := tx.WithContext(ctx).Model(&modelv1.ExecutionShardLease{}).Where("id = ?", row.Id).
		Select("floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint AS now_ms").Take(&clock).Error; err != nil {
		return nil, 0, err
	}
	if row.OwnerId != owner || row.Epoch != int64(epoch) || row.ExpiresAt <= clock.NowMS {
		return nil, 0, domain.ErrLeaseLost
	}
	return &row, clock.NowMS, nil
}

func lockSDKBudget(ctx context.Context, tx *gorm.DB, account string) (*modelv1.HimalayaBudget, error) {
	var row modelv1.HimalayaBudget
	err := tx.WithContext(ctx).Model(&modelv1.HimalayaBudget{}).Where("account_id = ? AND delete_at IS NULL", account).
		Clauses(clause.Locking{Strength: "UPDATE"}).First(&row).Error
	return &row, err
}

func budgetAmounts(row *modelv1.HimalayaBudget) (decimal.Decimal, decimal.Decimal, decimal.Decimal, error) {
	maximum, e1 := decimal.NewFromString(row.MaximumNotional)
	observed, e2 := decimal.NewFromString(row.ObservedNotional)
	reserved, e3 := decimal.NewFromString(row.ReservedNotional)
	if e1 != nil || e2 != nil || e3 != nil || !maximum.IsPositive() || observed.IsNegative() || reserved.IsNegative() {
		return decimal.Zero, decimal.Zero, decimal.Zero, ErrRiskLimit
	}
	return maximum, observed, reserved, nil
}

func checkReservedBudget(row *modelv1.HimalayaBudget) error {
	maximum, observed, reserved, err := budgetAmounts(row)
	if err != nil {
		return err
	}
	if observed.Add(reserved).GreaterThan(maximum) {
		return ErrRiskLimit
	}
	return nil
}

func requestDigest(request SubmitRequest) ([32]byte, error) {
	input, err := sdkInput(request)
	if err != nil {
		return [32]byte{}, err
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(input)
	return sha256.Sum256(encoded), err
}

func validStoreContext(ctx context.Context) bool {
	if ctx == nil || ctx.Err() != nil {
		return false
	}
	_, ok := ctx.Deadline()
	return ok
}
