package venue

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

// NewMatchingStore journals requests without process leases or a local fund
// reservation ledger. Matching is authoritative for live orders and funds.
// Legacy Store behavior remains available only to historical recovery tools.
func NewMatchingStore(db *gorm.DB, codec gateway.Codec) (*Store, error) {
	store, err := NewStore(db, codec)
	if err != nil {
		return nil, err
	}
	store.matching = true
	return store, nil
}

func (s *Store) prepareMatching(ctx context.Context, request SubmitRequest, live LiveAuthority) (Prepared, error) {
	if !validStoreContext(ctx) || live.InstanceID <= 0 || live.Revision <= 0 {
		return Prepared{}, ErrInvalidSubmit
	}
	request.IntentID = 1
	request.OwnerID, request.Epoch = "", 0
	digest, err := s.gateway.RequestDigest(request)
	if err != nil {
		return Prepared{}, err
	}
	row := &execution.VenueOrderIntent{
		AccountId:          AccountID(request.UserID, request.FundID),
		UserId:             request.UserID,
		FundId:             request.FundID,
		Symbol:             request.Symbol,
		ClientId:           request.ClientID,
		RequestDigest:      hex.EncodeToString(digest[:]),
		Side:               string(request.Side),
		Price:              request.Price.String(),
		Quantity:           request.Quantity.String(),
		Leverage:           request.Leverage,
		State:              IntentPrepared,
		ReservedNotional:   "0",
		StrategyInstanceId: &live.InstanceID,
		StrategyRevision:   &live.Revision,
		StrategyAccountId:  live.Strategy.GetAccountId(),
		SourceAccountId:    live.Book.GetPublisher().GetAccountId(),
		SourceUntil:        live.Book.GetValidUntilMs(),
	}
	deadline, _ := ctx.Deadline()
	row.ExpiresAt = min(deadline.UnixMilli(), time.Now().Add(maximumOperationTimeout).UnixMilli())
	err = s.db.WithContext(ctx).Model(&execution.VenueOrderIntent{}).Transaction(func(tx *gorm.DB) error {
		if _, err := matchingControls(ctx, tx, row); err != nil {
			return err
		}
		return tx.WithContext(ctx).Model(&execution.VenueOrderIntent{}).Create(row).Error
	})
	if err != nil {
		return Prepared{}, err
	}
	request.IntentID = row.Id
	return Prepared{
		Request: request,
		Admission: Admission{
			IntentID: row.Id,
			UserID:   request.UserID,
			FundID:   request.FundID,
			ClientID: request.ClientID,
			Digest:   digest,
		},
	}, nil
}

// matchingControls checks current configuration and observation freshness.
// No execution owner or reservation history is consulted.
func matchingControls(ctx context.Context, tx *gorm.DB, row *execution.VenueOrderIntent) (time.Duration, error) {
	if row.GetStrategyInstanceId() <= 0 || row.OwnerId != "" || row.OwnerEpoch != 0 ||
		row.StrategyAccountId != native.StrategyKey(row.UserId, row.FundId) || row.SourceAccountId != native.MarketKey(row.Symbol) {
		return 0, ErrInvalidSubmit
	}
	if err := lockCurrentInstance(ctx, tx, row.GetStrategyInstanceId(), row.GetStrategyRevision(), row.AccountId, row.Symbol, native.Source, domain.StrategyTemplateMarketMaker, domain.StrategyTemplateGrid); err != nil {
		return 0, err
	}
	var budget model.TradingAccountPolicy
	if err := tx.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Where("account_id = ? AND delete_at IS NULL", row.AccountId).
		Clauses(clause.Locking{Strength: "SHARE"}).Take(&budget).Error; err != nil {
		return 0, err
	}
	now := time.Now().UnixMilli()
	left := time.Duration(min(row.ExpiresAt, row.SourceUntil, budget.SnapshotExpiresAt)-now)*time.Millisecond - 50*time.Millisecond
	if left < minimumLiveSDKWindow || !budget.Enabled || !budget.CutoverCompleted || budget.UserId != row.UserId || budget.FundId != row.FundId || budget.Symbol != row.Symbol {
		return 0, ErrRiskLimit
	}
	price, e1 := domain.ParseLedgerDecimal(row.Price, false, false)
	quantity, e2 := domain.ParseLedgerDecimal(row.Quantity, false, false)
	maximum, e3 := domain.ParseLedgerDecimal(budget.MaximumNotional, false, false)
	observed, e4 := domain.ParseLedgerDecimal(budget.ObservedNotional, false, true)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || observed.Add(price.Mul(quantity)).GreaterThan(maximum) {
		return 0, ErrRiskLimit
	}
	if err := checkLiveBudget(&budget, SubmitRequest{
		Price:    price,
		Quantity: quantity,
		Leverage: row.Leverage,
	}, price.Mul(quantity)); err != nil {
		return 0, err
	}
	return left, nil
}

// The PREPARED -> DISPATCHED claim commits before the effect. Repeating this
// identity, including after a crash or lost reply, cannot send it again.
func (s *Store) withMatchingSubmission(ctx context.Context, admission Admission, send func(context.Context) error) error {
	if !validStoreContext(ctx) || send == nil || admission.IntentID <= 0 || admission.OwnerID != "" || admission.Epoch != 0 {
		return ErrInvalidSubmit
	}
	err := s.matchingIntent(ctx, admission, func(tx *gorm.DB, row *execution.VenueOrderIntent) error {
		if row.State != IntentPrepared {
			return domain.ErrOutcomeUnknown
		}
		if _, err := matchingControls(ctx, tx, row); err != nil {
			return err
		}
		return tx.WithContext(ctx).Model(&execution.VenueOrderIntent{}).Where("id = ?", row.Id).Update("state", IntentDispatched).Error
	})
	if err != nil {
		return err
	}
	var callErr error
	called := false
	err = s.matchingIntent(ctx, admission, func(tx *gorm.DB, row *execution.VenueOrderIntent) error {
		if row.State != IntentDispatched {
			return domain.ErrOutcomeUnknown
		}
		left, err := matchingControls(ctx, tx, row)
		if err != nil {
			return err
		}
		bounded, cancel := context.WithTimeout(ctx, left)
		defer cancel()
		called = true
		callErr = send(bounded)
		return nil
	})
	if !called || errors.Is(callErr, errSDKNotDispatched) {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), maximumOperationTimeout)
		defer cancel()
		err = errors.Join(err, s.matchingIntent(cleanup, admission, func(tx *gorm.DB, row *execution.VenueOrderIntent) error {
			if row.State != IntentDispatched || row.OrderId != "" {
				return domain.ErrOutcomeUnknown
			}
			return tx.WithContext(cleanup).Model(&execution.VenueOrderIntent{}).Where("id = ?", row.Id).Update("state", IntentTerminal).Error
		}))
	}
	return errors.Join(err, callErr)
}

func (s *Store) matchingIntent(ctx context.Context, admission Admission, apply func(*gorm.DB, *execution.VenueOrderIntent) error) error {
	return s.db.WithContext(ctx).Model(&execution.VenueOrderIntent{}).Transaction(func(tx *gorm.DB) error {
		var row execution.VenueOrderIntent
		if err := tx.WithContext(ctx).Model(&execution.VenueOrderIntent{}).Where("id = ? AND delete_at IS NULL", admission.IntentID).
			Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row).Error; err != nil {
			return err
		}
		if !s.matchesAdmission(&row, admission) || row.GetStrategyInstanceId() <= 0 || row.OwnerEpoch != 0 || row.OwnerId != "" {
			return ErrInvalidSubmit
		}
		return apply(tx, &row)
	})
}
