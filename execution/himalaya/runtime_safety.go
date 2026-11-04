package himalaya

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc/status"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	himalayav1 "github.com/hmlax/muskdeer/deer/proto/rpc/himalaya/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
)

func (r *Runtime) unresolvedCancels(ctx context.Context, account string) (bool, error) {
	var pending int64
	query, err := pendingSDKCancellations(ctx, r.db, account)
	if err != nil {
		return false, err
	}
	err = query.Where("state != ?", CancelConfirmed).Count(&pending).Error
	return pending != 0, err
}

func safeReason(err error) string {
	switch {
	case errors.Is(err, ErrRiskLimit):
		return "RISK_LIMIT"
	case errors.Is(err, domain.ErrLeaseLost):
		return "LEASE_LOST"
	case errors.Is(err, domain.ErrOutcomeUnknown):
		return "OUTCOME_UNKNOWN"
	case errors.Is(err, native.ErrInvalid), errors.Is(err, ErrInvalidSnapshot):
		return "OBSERVATION_INVALID"
	case errors.Is(err, context.DeadlineExceeded):
		return "DEADLINE_EXCEEDED"
	default:
		return status.Code(err).String()
	}
}

func (r *Runtime) validatePlanAuthority(ctx context.Context, profile *configv1.HimalayaProfile, plan *himalayav1.QuotePlan, book *commonv1.HimalayaBook) error {
	if plan.Book.GetSource() != native.Source || plan.Book.GetSegment() != native.Segment || plan.Book.GetSymbol() != profile.Symbol ||
		plan.Book.GetValidUntilMs() <= time.Now().UnixMilli() || book.Publisher.OwnerId != plan.Book.GetPublisher().GetOwnerId() || book.Publisher.Epoch != plan.Book.Publisher.Epoch ||
		plan.Strategy.AccountId != native.StrategyKey(profile.UserId, profile.FundId) || repository.CheckNativeAuthority(ctx, r.db, plan.Strategy) != nil {
		return domain.ErrLeaseLost
	}
	return nil
}

func (r *Runtime) targets(profile *configv1.HimalayaProfile, plan *himalayav1.QuotePlan) (map[string]*SubmitRequest, error) {
	if len(plan.Book.Bids) == 0 || len(plan.Book.Asks) == 0 {
		return nil, native.ErrInvalid
	}
	tick, err := native.Value(profile.PriceTick, false, false)
	if err != nil {
		return nil, err
	}
	step, err := native.Value(profile.QuantityStep, false, false)
	if err != nil {
		return nil, err
	}
	limit, err := domain.ParseLedgerDecimal(profile.MaximumOrderNotional, false, false)
	if err != nil {
		return nil, err
	}
	result := make(map[string]*SubmitRequest)
	for _, proposal := range plan.Targets {
		price, e1 := native.Value(proposal.GetPrice(), false, false)
		quantity, e2 := native.Value(proposal.GetQuantity(), false, false)
		side := domain.Side(proposal.GetSide())
		if e1 != nil || e2 != nil || side != domain.SideBuy && side != domain.SideSell || price.Mul(quantity).GreaterThan(limit) {
			return nil, ErrRiskLimit
		}
		if _, remainder := price.QuoRem(tick, 0); !remainder.IsZero() {
			return nil, ErrInvalidSubmit
		}
		if _, remainder := quantity.QuoRem(step, 0); !remainder.IsZero() {
			return nil, ErrInvalidSubmit
		}
		levels := plan.Book.Bids
		if side == domain.SideSell {
			levels = plan.Book.Asks
		}
		first, e1 := native.Value(levels[0].Price, false, false)
		last, e2 := native.Value(levels[len(levels)-1].Price, false, false)
		if e1 != nil || e2 != nil {
			return nil, native.ErrInvalid
		}
		first, e1 = domain.RoundPrice(first, tick, side)
		last, e2 = domain.RoundPrice(last, tick, side)
		if e1 != nil || e2 != nil || price.LessThan(decimal.Min(first, last)) || price.GreaterThan(decimal.Max(first, last)) {
			return nil, native.ErrInvalid
		}
		key := string(side) + "/" + price.String()
		if result[key] != nil {
			return nil, ErrInvalidSubmit
		}
		result[key] = &SubmitRequest{
			Price:    price,
			Quantity: quantity,
			Side:     side,
		}
	}
	return result, nil
}

func orderKey(order *commonv1.HimalayaOrder) (string, decimal.Decimal, error) {
	price, e1 := native.Value(order.Price, false, false)
	quantity, e2 := native.Value(order.Quantity, false, false)
	executed, e3 := native.Value(order.ExecutedQuantity, false, true)
	if e1 != nil || e2 != nil || e3 != nil || executed.GreaterThan(quantity) {
		return "", decimal.Zero, native.ErrInvalid
	}
	return order.Side + "/" + price.String(), quantity.Sub(executed), nil
}

func (r *Runtime) protect(ctx context.Context, profile *configv1.HimalayaProfile) error {
	lease, err := r.acquire(ctx, profile)
	if err != nil {
		return err
	}
	book, bookErr := r.book(ctx, profile.Symbol)
	_, _, readErr := r.read(ctx, profile)
	budget, err := r.store.Budget(ctx, lease.Key.AccountID)
	if err != nil {
		return err
	}
	if bookErr != nil || readErr != nil || !budget.Enabled || r.plans[lease.Key.AccountID].Load() <= time.Now().UnixMilli() {
		return r.cancelManaged(ctx, profile)
	}
	_, err = r.refresh(ctx, profile, lease, book)
	return err
}

func (r *Runtime) cancelManaged(ctx context.Context, profile *configv1.HimalayaProfile) error {
	lease, err := r.acquire(ctx, profile)
	if err != nil {
		return err
	}
	raw, err := r.client.Load().Read(ctx, Scope{
		UserID: profile.UserId,
		FundID: profile.FundId,
		Symbol: profile.Symbol,
	})
	if err != nil {
		return err
	}
	budget, err := r.store.Budget(ctx, lease.Key.AccountID)
	if err != nil {
		return err
	}
	for _, row := range raw.Orders {
		if !native.Active(row.Status) {
			continue
		}
		owned := strings.HasPrefix(row.ClientId, native.ClientPrefix+profile.Symbol+"_")
		legacy := !budget.CutoverCompleted && strings.HasPrefix(row.ClientId, native.LegacyPrefix+profile.Symbol+"_")
		if !owned && !legacy {
			continue
		}
		order, err := normalizeOrder(row, Scope{
			UserID: profile.UserId,
			FundID: profile.FundId,
			Symbol: profile.Symbol,
		}, -int32(raw.Market.GetMarket().GetEffectiveBasePrecision()))
		if err != nil {
			return err
		}
		if err := r.cancelOne(ctx, lease, profile, order); err != nil {
			return err
		}
	}
	_, err = r.refresh(ctx, profile, lease, nil)
	return err
}
