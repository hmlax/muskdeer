package usecase

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/hmlax/muskdeer/internal/domain"
)

const maxActionObservationsPerTick = 8

func (s *StrategyController) recoverActions(ctx context.Context, spec domain.StrategySpec, authority domain.StrategyAuthority) (bool, error) {
	actions, err := s.deps.Actions.Active(ctx, spec.ID)
	if err != nil {
		return false, err
	}
	for _, action := range actions {
		if action.State != domain.StrategyActionPending {
			continue
		}
		if action.Plan.Kind == domain.StrategyActionSubmit {
			// A crash can leave an unsent target behind. Discard it and compute
			// from current input; attempted actions never take this path.
			_, err := s.deps.Actions.Discard(ctx, action, authority)
			return true, err
		}
		return true, s.dispatch(ctx, action, authority)
	}
	start := 0
	for index, action := range actions {
		if action.ID > s.afterAction {
			start = index
			break
		}
	}
	for offset := range min(len(actions), maxActionObservationsPerTick) {
		action := actions[(start+offset)%len(actions)]
		s.afterAction = action.ID
		if err := s.observeAction(ctx, action, authority); err != nil {
			return false, err
		}
	}
	return false, nil
}

func (s *StrategyController) observeAction(ctx context.Context, action domain.StrategyAction, authority domain.StrategyAuthority) error {
	var err error
	if action.Plan.Kind == domain.StrategyActionSubmit {
		_, err = s.deps.Execution.FindOrder(ctx, action.Plan.AccountID, action.IdempotencyKey)
	} else {
		_, err = s.deps.Execution.FindCancellation(ctx, action.Plan.AccountID, action.IdempotencyKey)
	}
	if err != nil && !errors.Is(err, domain.ErrCoreNotFound) && !errors.Is(err, domain.ErrOutcomeUnknown) {
		return err
	}
	if err != nil && ctx.Err() != nil {
		return errStrategyReobserve
	}
	// NotFound and transport failure both retain uncertainty. This read cannot
	// authorize a resend, discard, or a replacement submit with a new key.
	_, err = s.deps.Actions.Observe(ctx, action, authority)
	return err
}

func (s *StrategyController) dispatch(ctx context.Context, pending domain.StrategyAction, authority domain.StrategyAuthority) error {
	native, _ := ctx.Deadline()
	action, err := s.deps.Actions.Attempt(ctx, pending, authority, native.Truncate(time.Millisecond))
	if errors.Is(err, domain.ErrRiskDenied) {
		// A stop can commit after quoting or Plan. No execution call occurred;
		// keep any PENDING action durable for the next tick to discard.
		return errStrategyReobserve
	}
	if err != nil {
		return localStrategyFailure(ctx, err)
	}
	if action.Plan.Kind == domain.StrategyActionSubmit {
		_, err = s.deps.Execution.Submit(ctx, domain.OrderInput{
			RequestID:      action.Plan.Key,
			IdempotencyKey: action.IdempotencyKey,
			Authority:      authority,
			AccountID:      action.Plan.AccountID,
			InstrumentID:   action.Plan.InstrumentID,
			Side:           action.Plan.Side,
			Price:          action.Plan.Price,
			Quantity:       action.Plan.Quantity,
			TimeInForce:    action.Plan.TimeInForce,
			ReduceOnly:     action.Plan.ReduceOnly,
			Deadline:       action.Deadline,
		})
	} else {
		_, err = s.deps.Execution.Cancel(ctx, domain.CancelInput{
			RequestID:      action.Plan.Key,
			IdempotencyKey: action.IdempotencyKey,
			Authority:      authority,
			AccountID:      action.Plan.AccountID,
			InstrumentID:   action.Plan.InstrumentID,
			ClientOrderID:  action.Plan.TargetClientOrderID,
			Deadline:       action.Deadline,
		})
	}
	if action.Plan.Kind == domain.StrategyActionSubmit && errors.Is(err, domain.ErrRiskDenied) {
		// A newer admission decision can reject risk after Attempt was durable.
		// Keep that identity; a fresh tick queries it, and only the existing
		// deadline/absence proof may resolve it. Never create a replacement here.
		return errStrategyReobserve
	}
	if err != nil && !errors.Is(err, domain.ErrOutcomeUnknown) {
		return err
	}
	if err != nil && ctx.Err() != nil {
		// Attempt is durable. A fresh tick must observe the original key;
		// there is no remaining request budget for another database operation.
		return errStrategyReobserve
	}
	_, err = s.deps.Actions.Observe(ctx, action, authority)
	return err
}

func (s *StrategyController) planAndDispatch(ctx context.Context, authority domain.StrategyAuthority, plan domain.StrategyActionPlan) error {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	plan.Key = hex.EncodeToString(random[:])
	stored, err := s.deps.Actions.Plan(ctx, authority, plan)
	if errors.Is(err, domain.ErrRiskDenied) {
		// Admission has observed newer lifecycle intent. Re-read that intent
		// using the next bounded tick instead of killing the account worker.
		return errStrategyReobserve
	}
	if err != nil {
		return localStrategyFailure(ctx, err)
	}
	return s.dispatch(ctx, stored, authority)
}

// Only this call's actual deadline permits a new observation cycle. Local
// commit uncertainty retains the journal, and never authorizes a remote write.
func localStrategyFailure(ctx context.Context, err error) error {
	if ctx == nil || !errors.Is(ctx.Err(), context.DeadlineExceeded) || !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	for _, critical := range []error{domain.ErrLeaseLost, domain.ErrInvalidOrder, domain.ErrIdempotencyConflict, domain.ErrInvalidParameters, domain.ErrForbidden, context.Canceled} {
		if errors.Is(err, critical) {
			return err
		}
	}
	return errStrategyReobserve
}

func (s *StrategyController) currentOrders(ctx context.Context, spec domain.StrategySpec) ([]domain.IntentSnapshot, error) {
	filter := domain.ExecutionFilter{
		AccountID:    spec.AccountID,
		StrategyID:   spec.ID,
		InstrumentID: spec.InstrumentID,
		Limit:        domain.MaximumExecutionPage,
		OpenOnly:     true,
	}
	orders := make([]domain.IntentSnapshot, 0)
	seen := 0
	for pages := 0; pages <= domain.MaxStrategyOpenOrders/domain.MaximumExecutionPage; pages++ {
		page, next, err := s.deps.Execution.Orders(ctx, filter)
		if err != nil {
			if errors.Is(err, domain.ErrOutcomeUnknown) {
				return nil, errStrategyReobserve
			}
			return nil, err
		}
		seen += len(page)
		if seen > domain.MaxStrategyOpenOrders || next > 0 && next <= filter.AfterID {
			return nil, domain.ErrRiskDenied
		}
		for _, row := range page {
			observed, err := s.deps.Execution.Order(ctx, spec.AccountID, row.ClientOrderID)
			if err != nil {
				if errors.Is(err, domain.ErrOutcomeUnknown) {
					return nil, errStrategyReobserve
				}
				return nil, err
			}
			if observed.ID != row.ID || observed.Input.Authority.StrategyID != spec.ID || observed.Input.InstrumentID != spec.InstrumentID {
				return nil, domain.ErrInvalidOrder
			}
			if !terminalOrder(observed.State) {
				orders = append(orders, observed)
			}
		}
		if next == 0 {
			return orders, nil
		}
		filter.AfterID = next
	}
	return nil, domain.ErrRiskDenied
}
