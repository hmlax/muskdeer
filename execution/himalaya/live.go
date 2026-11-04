package himalaya

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	commonmodelv1 "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
)

const minimumLiveSDKWindow = 500 * time.Millisecond

// LiveAuthority binds one source observation and the current strategy owner.
type LiveAuthority struct {
	Strategy *commonv1.HimalayaAuthority
	Book     *commonv1.HimalayaBook
}

// PrepareLive admits only current selected-source plans and margin obligations.
func (s *Store) PrepareLive(ctx context.Context, execution domain.LeaseToken, request SubmitRequest, authority LiveAuthority) (Prepared, error) {
	if authority.Strategy.GetAccountId() != native.StrategyKey(request.UserID, request.FundID) ||
		authority.Book.GetSymbol() != request.Symbol || authority.Book.GetSource() != native.Source || authority.Book.GetSegment() != native.Segment ||
		authority.Book.GetPublisher().GetAccountId() != native.MarketKey(request.Symbol) {
		return Prepared{}, ErrInvalidSubmit
	}
	return s.prepare(ctx, execution, request, &authority)
}

func setLiveAuthority(ctx context.Context, row *executionmodelv1.VenueOrderIntent, authority *LiveAuthority, now int64) {
	deadline, _ := ctx.Deadline()
	row.ExpiresAt = now + min(time.Until(deadline), maximumOperationTimeout).Milliseconds()
	row.StrategyAccountId = authority.Strategy.AccountId
	row.StrategyOwnerId = authority.Strategy.OwnerId
	row.StrategyEpoch = authority.Strategy.Epoch
	row.SourceAccountId = authority.Book.Publisher.AccountId
	row.SourceOwnerId = authority.Book.Publisher.OwnerId
	row.SourceEpoch = authority.Book.Publisher.Epoch
	row.SourceUntil = authority.Book.ValidUntilMs
}

func checkLiveAuthority(ctx context.Context, tx *gorm.DB, row *executionmodelv1.VenueOrderIntent, now int64) (time.Duration, error) {
	if row.StrategyEpoch == 0 && row.SourceEpoch == 0 {
		return maximumOperationTimeout, nil // The original transport-only seam.
	}
	remaining := time.Duration(min(row.ExpiresAt, row.SourceUntil)-now)*time.Millisecond - 50*time.Millisecond
	if remaining <= 0 || row.StrategyAccountId != native.StrategyKey(row.UserId, row.FundId) || row.SourceAccountId != native.MarketKey(row.Symbol) {
		return 0, domain.ErrLeaseLost
	}
	for _, token := range []*commonv1.HimalayaAuthority{
		{
			AccountId:   row.StrategyAccountId,
			OwnerId:     row.StrategyOwnerId,
			Epoch:       row.StrategyEpoch,
			ExpiresAtMs: row.ExpiresAt,
		},
		{
			AccountId:   row.SourceAccountId,
			OwnerId:     row.SourceOwnerId,
			Epoch:       row.SourceEpoch,
			ExpiresAtMs: min(row.ExpiresAt, row.SourceUntil),
		},
	} {
		left, err := repository.LockNativeAuthority(ctx, tx, token)
		if err != nil {
			return 0, err
		}
		remaining = min(remaining, left)
	}
	return remaining, nil
}

func checkLiveBudget(budget *commonmodelv1.TradingAccountPolicy, request SubmitRequest, total decimal.Decimal) error {
	available, e1 := domain.ParseLedgerDecimal(budget.AvailableQuote, false, true)
	limit, e2 := domain.ParseLedgerDecimal(budget.MaximumOrderNotional, false, false)
	fee, e3 := domain.ParseLedgerDecimal(budget.WorstFeeRate, false, true)
	if e1 != nil || e2 != nil || e3 != nil || budget.Leverage != request.Leverage || request.Price.Mul(request.Quantity).GreaterThan(limit) {
		return ErrRiskLimit
	}
	margin, remainder := total.QuoRem(decimal.NewFromInt(int64(request.Leverage)), 18)
	if !remainder.IsZero() {
		margin = margin.Add(decimal.New(1, -18))
	}
	if margin.Add(total.Mul(fee)).GreaterThan(available) {
		return ErrRiskLimit
	}
	return nil
}

func checkPersistedLiveBudget(budget *commonmodelv1.TradingAccountPolicy, row *executionmodelv1.VenueOrderIntent) error {
	if row.SourceEpoch == 0 {
		return nil
	}
	reserved, err := domain.ParseLedgerDecimal(budget.ReservedNotional, false, true)
	if err != nil {
		return err
	}
	price, e1 := domain.ParseLedgerDecimal(row.Price, false, false)
	quantity, e2 := domain.ParseLedgerDecimal(row.Quantity, false, false)
	if e1 != nil || e2 != nil || !budget.CutoverCompleted {
		return ErrRiskLimit
	}
	return checkLiveBudget(budget, SubmitRequest{
		Price:    price,
		Quantity: quantity,
		Leverage: row.Leverage,
	}, reserved)
}

func liveSubmissionWindow(ctx context.Context, row *executionmodelv1.VenueOrderIntent, remaining time.Duration) (time.Duration, error) {
	if row.SourceEpoch == 0 {
		return remaining, nil
	}
	deadline, bounded := ctx.Deadline()
	if !bounded {
		return 0, domain.ErrLeaseLost
	}
	remaining = min(remaining, time.Until(deadline))
	if remaining < minimumLiveSDKWindow {
		return 0, domain.ErrLeaseLost
	}
	return remaining, nil
}
