package venue

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc/status"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	executionvenuev1 "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
	"github.com/hmlax/muskdeer/deer/trading/strategyconfig"
)

func (r *Runtime) unresolvedCancels(ctx context.Context, account string) (bool, error) {
	if r.cfg.InstanceID > 0 {
		return false, nil // Current venue orders, rather than historical receipts, govern quotation.
	}
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

func (r *Runtime) validatePlanAuthority(ctx context.Context, profile *commonv1.VenueProfilePolicy, plan *executionvenuev1.QuotePlan, book *commonv1.VenueBook) error {
	if plan.GetStrategyInstanceId() != r.cfg.InstanceID || plan.GetStrategyRevision() != r.cfg.Revision {
		return domain.ErrLeaseLost
	}
	if r.store.matching {
		if plan.Book.GetSource() != native.Source || plan.Book.GetSegment() != native.Segment || plan.Book.GetSymbol() != profile.Symbol ||
			plan.Book.GetValidUntilMs() <= time.Now().UnixMilli() || book.GetPublisher().GetAccountId() != native.MarketKey(profile.Symbol) ||
			plan.Book.GetPublisher().GetAccountId() != native.MarketKey(profile.Symbol) || plan.GetStrategy().GetAccountId() != native.StrategyKey(profile.UserId, profile.FundId) {
			return native.ErrInvalid
		}
		return nil
	}
	if plan.Book.GetSource() != native.Source || plan.Book.GetSegment() != native.Segment || plan.Book.GetSymbol() != profile.Symbol ||
		plan.Book.GetValidUntilMs() <= time.Now().UnixMilli() || book.Publisher.OwnerId != plan.Book.GetPublisher().GetOwnerId() || book.Publisher.Epoch != plan.Book.Publisher.Epoch ||
		plan.Strategy.AccountId != native.StrategyKey(profile.UserId, profile.FundId) || repository.CheckNativeAuthority(ctx, r.db, plan.Strategy) != nil {
		return domain.ErrLeaseLost
	}
	return nil
}

func (r *Runtime) targets(profile *commonv1.VenueProfilePolicy, plan *executionvenuev1.QuotePlan) (map[string]*SubmitRequest, error) {
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
		if r.cfg.Grid != nil {
			if err := r.validateGridTarget(plan.Book, proposal); err != nil {
				return nil, err
			}
		} else {
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

func orderKey(order *commonv1.VenueOrder) (string, decimal.Decimal, error) {
	price, e1 := native.Value(order.Price, false, false)
	quantity, e2 := native.Value(order.Quantity, false, false)
	executed, e3 := native.Value(order.ExecutedQuantity, false, true)
	if e1 != nil || e2 != nil || e3 != nil || executed.GreaterThan(quantity) {
		return "", decimal.Zero, native.ErrInvalid
	}
	return order.Side + "/" + price.String(), quantity.Sub(executed), nil
}

func (r *Runtime) protect(ctx context.Context, profile *commonv1.VenueProfilePolicy) error {
	lease, err := r.acquire(ctx, profile)
	if err != nil {
		return err
	}
	active, err := instanceconfig.Active(ctx, r.db, r.cfg.InstanceID, r.cfg.Revision)
	if err != nil {
		return err
	}
	if !active {
		return r.cancelManaged(ctx, profile)
	}
	_, snapshot, readErr := r.read(ctx, profile)
	book, bookErr := r.book(ctx, profile.Symbol)
	if bookErr != nil || readErr != nil {
		return r.cancelManaged(ctx, profile)
	}
	return r.protectObserved(ctx, profile, quoteObservation{
		lease:    lease,
		book:     book,
		snapshot: snapshot,
	})
}

func (r *Runtime) protectObserved(ctx context.Context, profile *commonv1.VenueProfilePolicy, observation quoteObservation) error {
	lease, book, snapshot := observation.lease, observation.book, observation.snapshot
	budget, err := r.store.Budget(ctx, lease.Key.AccountID)
	if err != nil {
		return err
	}
	if !budget.Enabled || !r.currentQuotePlan(ctx, book, lease.Key.AccountID) {
		return r.cancelManaged(ctx, profile)
	}
	if r.cfg.InstanceID > 0 {
		if !r.directMaker() {
			if err := r.store.Observe(ctx, lease, snapshot); err != nil {
				return err
			}
			if err := r.syncSnapshotBudget(ctx, lease, snapshot, book); err != nil {
				return err
			}
		}
		retained := &quoteRetention{orders: make(map[string]bool)}
		bid, e1 := native.Value(book.Bids[0].Price, false, false)
		ask, e2 := native.Value(book.Asks[0].Price, false, false)
		if e1 != nil || e2 != nil || !bid.LessThan(ask) {
			return r.cancelManaged(ctx, profile)
		}
		best := map[domain.Side]decimal.Decimal{
			domain.SideBuy:  bid,
			domain.SideSell: ask,
		}
		limit, err := native.Value(profile.MaximumOrderNotional, false, false)
		if err != nil {
			return err
		}
		for _, order := range snapshot.Orders {
			if _, safe := bridgePrice(order, best, limit); safe {
				retained.orders[order.OrderId] = true
			}
		}
		return r.cancelObsolete(ctx, profile, lease, snapshot, retained, &executionvenuev1.QuoteResult{}, r.quoteCutover(budget.CutoverCompleted))
	}
	_, err = r.refresh(ctx, profile, lease, book)
	return err
}

func (r *Runtime) cancelManaged(ctx context.Context, profile *commonv1.VenueProfilePolicy) error {
	if r.cfg.LongGrid != nil {
		_, snapshot, err := r.read(ctx, profile)
		if err != nil {
			return err
		}
		_, err = r.cancelGridOrders(ctx, snapshot)
		return err
	}
	lease, err := r.acquire(ctx, profile)
	if err != nil {
		return err
	}
	orders, err := r.client.Load().ReadOrders(ctx, Scope{
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
	for _, order := range orders {
		if !native.Active(order.Status) {
			continue
		}
		if !ownsCancellation(profile, order.ClientId, r.quoteCutover(budget.CutoverCompleted)) {
			continue
		}
		if !r.directMaker() {
			if err := r.store.Observe(ctx, lease, &commonv1.VenueSnapshot{
				Scope:  native.Scope(profile),
				Orders: []*commonv1.VenueOrder{order},
			}); err != nil {
				return err
			}
		}
		if err := r.cancelOne(ctx, lease, profile, order); err != nil {
			return err
		}
	}
	_, err = r.refresh(ctx, profile, lease, nil)
	return err
}

func (r *Runtime) maximumTargets() int {
	if r.cfg.Grid != nil {
		return int(r.cfg.Grid.GridCount)
	}
	return 40
}
func (r *Runtime) validateGridTarget(book *commonv1.VenueBook, target *commonv1.VenueTarget) error {
	p, err := strategyconfig.Grid(r.cfg.Grid)
	if err != nil {
		return err
	}
	if time.Since(time.UnixMilli(book.EventAtMs)) > p.MaxMarketAge {
		return native.ErrInvalid
	}
	tick, err := native.Value(r.cfg.Venue.Profiles[0].PriceTick, false, false)
	if err != nil {
		return err
	}
	bid, e1 := native.Value(book.Bids[0].Price, false, false)
	ask, e2 := native.Value(book.Asks[0].Price, false, false)
	if e1 != nil || e2 != nil {
		return native.ErrInvalid
	}
	mid, _ := bid.Add(ask).QuoRem(decimal.NewFromInt(2), 19)
	levels, err := domain.GridPrices(p, tick, mid, decimal.Zero, decimal.Zero)
	if err != nil {
		return err
	}
	price, _ := native.Value(target.Price, false, false)
	quantity, _ := native.Value(target.Quantity, false, false)
	if price.Mul(quantity).GreaterThan(p.PerOrderNotional) {
		return ErrRiskLimit
	}
	for _, level := range levels {
		if string(level.Side) == target.Side && level.Price.Equal(price) {
			return nil
		}
	}
	return native.ErrInvalid
}
