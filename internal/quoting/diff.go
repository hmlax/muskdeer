package quoting

import (
	"cmp"
	"slices"
	"time"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/internal/domain"
)

// DiffInput is a complete, bounded observation of one instance's orders.
// LastRefresh is the last committed controller decision, not a market timestamp.
// Protect bypasses refresh throttling and never permits new submissions.
type DiffInput struct {
	StrategyID         int64
	AccountID          string
	InstrumentID       int64
	Targets            []Target
	Orders             []domain.IntentSnapshot
	Now                time.Time
	LastRefresh        time.Time
	RepriceBPS         decimal.Decimal
	MinRefreshInterval time.Duration
	MaxQuoteAge        time.Duration
	Protect            bool
}

// Plan contains proposed actions. Cancellation acceptance is not terminal;
// Observe requires execution reconciliation, without inventing a missing order.
type Plan struct {
	Keep    []int64
	Cancel  []int64
	Observe []int64
	Submit  []Target
}

// Diff retains compatible quotes and cancels surplus, changed or expired orders.
// Any cancellation or uncertainty blocks ALL new submissions in this decision.
// A later, complete observation must prove terminal state before replacement.
func Diff(input DiffInput) (Plan, error) {
	if err := validateDiff(input); err != nil {
		return Plan{}, err
	}
	orders := slices.Clone(input.Orders)
	slices.SortFunc(orders, func(a, b domain.IntentSnapshot) int { return cmp.Compare(a.ID, b.ID) })
	targets := slices.Clone(input.Targets)
	slices.SortFunc(targets, func(a, b Target) int {
		if a.Level != b.Level {
			return cmp.Compare(a.Level, b.Level)
		}
		return cmp.Compare(a.Side, b.Side)
	})
	plan := Plan{}
	candidates := make([]domain.IntentSnapshot, 0, len(orders))
	throttled := !input.LastRefresh.IsZero() && input.Now.Sub(input.LastRefresh) < input.MinRefreshInterval
	for _, order := range orders {
		if terminal(order.State) {
			continue
		}
		if input.Protect && order.State == domain.OrderStateIntent && (order.CancelState == domain.CancelStateNone || order.CancelState == domain.CancelStateRejected) {
			plan.Cancel = append(plan.Cancel, order.ID)
			continue
		}
		if uncertain(order) {
			plan.Observe = append(plan.Observe, order.ID)
			continue
		}
		if input.Protect || input.Now.Sub(order.CreatedAt) >= input.MaxQuoteAge {
			plan.Cancel = append(plan.Cancel, order.ID)
			continue
		}
		if throttled {
			plan.Keep = append(plan.Keep, order.ID)
			continue
		}
		candidates = append(candidates, order)
	}
	if input.Protect || throttled {
		return plan, nil
	}
	used := make([]bool, len(candidates))
	missing := make([]Target, 0, len(targets))
	for _, target := range targets {
		match := -1
		distance := decimal.Zero
		for i, order := range candidates {
			if used[i] || !compatible(order, target, input.RepriceBPS) {
				continue
			}
			delta := order.Input.Price.Sub(target.Price).Abs()
			if match < 0 || delta.LessThan(distance) {
				match, distance = i, delta
			}
		}
		if match < 0 {
			missing = append(missing, target)
			continue
		}
		used[match] = true
		plan.Keep = append(plan.Keep, candidates[match].ID)
	}
	for i, order := range candidates {
		if !used[i] {
			plan.Cancel = append(plan.Cancel, order.ID)
		}
	}
	slices.Sort(plan.Keep)
	slices.Sort(plan.Cancel)
	if len(plan.Cancel) == 0 && len(plan.Observe) == 0 {
		plan.Submit = missing
	}
	return plan, nil
}

func compatible(order domain.IntentSnapshot, target Target, reprice decimal.Decimal) bool {
	return order.Input.Side == target.Side && order.Input.TimeInForce == target.TimeInForce &&
		order.Input.Quantity.Sub(order.FilledQuantity).Equal(target.Quantity) &&
		order.Input.Price.Sub(target.Price).Abs().Mul(decimal.NewFromInt(10000)).LessThanOrEqual(target.Price.Mul(reprice))
}

func uncertain(order domain.IntentSnapshot) bool {
	return order.State == domain.OrderStateIntent || order.State == domain.OrderStateSubmitting || order.State == domain.OrderStateUnknown ||
		order.CancelState == domain.CancelStateRequested || order.CancelState == domain.CancelStatePending || order.CancelState == domain.CancelStateUnknown
}

func terminal(state domain.OrderState) bool {
	return state == domain.OrderStateFilled || state == domain.OrderStateCanceled || state == domain.OrderStateRejected
}

func validateDiff(input DiffInput) error {
	if input.StrategyID <= 0 || input.AccountID == "" || len(input.AccountID) > 64 || input.InstrumentID <= 0 || input.Now.UnixMilli() <= 0 || input.LastRefresh.After(input.Now) ||
		input.MinRefreshInterval < time.Millisecond || input.MaxQuoteAge < input.MinRefreshInterval || input.MaxQuoteAge > time.Hour ||
		!domain.FixedDecimal(input.RepriceBPS, 5, 10, true) || input.RepriceBPS.GreaterThan(decimal.NewFromInt(10000)) || len(input.Targets) > 40 || len(input.Orders) > 200 {
		return ErrInvalidInput
	}
	if err := validateTargets(input.Targets); err != nil {
		return err
	}
	return validateObservedOrders(input)
}

func validateTargets(targets []Target) error {
	seenTargets := make(map[struct {
		side  domain.Side
		level int
	}]struct{}, len(targets))
	for _, target := range targets {
		key := struct {
			side  domain.Side
			level int
		}{
			side:  target.Side,
			level: target.Level,
		}
		if _, exists := seenTargets[key]; exists {
			return ErrInvalidInput
		}
		seenTargets[key] = struct{}{}
		if target.Level < 0 || target.Level >= 20 || (target.Side != domain.SideBuy && target.Side != domain.SideSell) || target.TimeInForce != domain.TimeInForcePostOnly ||
			!domain.FixedDecimal(target.Price, 20, 18, false) || !domain.FixedDecimal(target.Quantity, 20, 18, false) {
			return ErrInvalidInput
		}
	}
	return nil
}

func validateObservedOrders(input DiffInput) error {
	seenOrders := make(map[int64]struct{}, len(input.Orders))
	for _, order := range input.Orders {
		if _, exists := seenOrders[order.ID]; exists {
			return ErrInvalidInput
		}
		seenOrders[order.ID] = struct{}{}
		if order.ID <= 0 || order.Input.Authority.StrategyID != input.StrategyID || order.Input.AccountID != input.AccountID || order.Input.InstrumentID != input.InstrumentID ||
			(order.Input.Side != domain.SideBuy && order.Input.Side != domain.SideSell) || (order.Input.TimeInForce != domain.TimeInForcePostOnly && order.Input.TimeInForce != domain.TimeInForceGTC) ||
			!domain.FixedDecimal(order.Input.Price, 20, 18, false) || !domain.FixedDecimal(order.Input.Quantity, 20, 18, false) ||
			!domain.FixedDecimal(order.FilledQuantity, 20, 18, true) || order.FilledQuantity.GreaterThan(order.Input.Quantity) ||
			order.CreatedAt.UnixMilli() <= 0 || order.UpdatedAt.Before(order.CreatedAt) || order.UpdatedAt.After(input.Now.Add(time.Second)) ||
			!validOrderState(order.State) || !validCancelState(order.CancelState) ||
			!terminal(order.State) && (order.FilledQuantity.Equal(order.Input.Quantity) || order.CancelState == domain.CancelStateConfirmed) {
			return ErrInvalidInput
		}
	}
	return nil
}

func validOrderState(state domain.OrderState) bool {
	switch state {
	case domain.OrderStateIntent, domain.OrderStateSubmitting, domain.OrderStateUnknown, domain.OrderStateLive, domain.OrderStatePartiallyFilled, domain.OrderStateFilled, domain.OrderStateCanceled, domain.OrderStateRejected:
		return true
	default:
		return false
	}
}

func validCancelState(state domain.CancelState) bool {
	switch state {
	case domain.CancelStateNone, domain.CancelStateRequested, domain.CancelStatePending, domain.CancelStateUnknown, domain.CancelStateConfirmed, domain.CancelStateRejected:
		return true
	default:
		return false
	}
}
