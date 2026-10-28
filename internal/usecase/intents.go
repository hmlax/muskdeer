// Package usecase coordinates domain repositories and gateways without knowing
// their transport, framework or database implementations.
package usecase

import (
	"context"
	"errors"

	"github.com/hmlax/muskdeer/internal/domain"
)

type intentService struct {
	store   domain.IntentExecutionRepository
	gateway domain.TradingGateway
}

type unknownOutcome struct{ cause error }

func (*unknownOutcome) Error() string { return domain.MessageOutcomeUnknown }
func (e *unknownOutcome) Unwrap() error {
	return errors.Join(domain.ErrOutcomeUnknown, e.cause)
}

// NewIntentService owns no worker or connection. The caller owns its request
// budget; recovery uses a fresh bounded call and the original durable identity.
func NewIntentService(store domain.IntentExecutionRepository, gateway domain.TradingGateway) (domain.IntentService, error) {
	if store == nil || gateway == nil {
		return nil, domain.ErrInvalidOrder
	}
	return &intentService{
		store:   store,
		gateway: gateway,
	}, nil
}

func (s *intentService) Submit(ctx context.Context, input domain.OrderInput, execution domain.LeaseToken) (domain.IntentSnapshot, error) {
	intent, err := s.store.Accept(ctx, input, execution)
	if err != nil {
		return domain.IntentSnapshot{}, err
	}
	if intent.State != domain.OrderStateIntent {
		return s.Recover(ctx, input.AccountID, intent.ID)
	}
	prepared, err := s.store.Prepare(ctx, intent, input.Authority, execution, input.Deadline)
	if err != nil {
		if errors.Is(err, domain.ErrIdempotencyConflict) || errors.Is(err, domain.ErrOutcomeUnknown) {
			return s.Recover(ctx, input.AccountID, intent.ID)
		}
		if errors.Is(err, domain.ErrRiskDenied) || errors.Is(err, domain.ErrInvalidOrder) {
			return s.store.Reject(ctx, intent, domain.IntentErrorRiskDenied)
		}
		return intent, err
	}
	attemptCtx, cancel := context.WithDeadline(ctx, prepared.Input.Deadline)
	defer cancel()
	coreInput := prepared.Input
	coreInput.IdempotencyKey = domain.CoreSubmitKeyPrefix + prepared.ClientOrderID
	order, err := s.gateway.Submit(attemptCtx, domain.GatewaySubmit{
		IntentID:           prepared.ID,
		Input:              coreInput,
		ClientOrderID:      prepared.ClientOrderID,
		ExecutionAuthority: prepared.ExecutionAuthority,
	})
	if err != nil {
		if confirmedRejection(err) {
			code := domain.IntentErrorCoreRejected
			if errors.Is(err, domain.ErrRiskDenied) {
				code = domain.IntentErrorRiskDenied
			}
			return s.store.Reject(ctx, prepared, code)
		}
		return s.keepUnknown(ctx, prepared, domain.IntentErrorOutcomeUnknown, err)
	}
	confirmed, err := s.store.Confirm(ctx, prepared, order)
	if err != nil {
		if errors.Is(err, domain.ErrIdempotencyConflict) {
			return s.store.ByID(ctx, input.AccountID, intent.ID)
		}
		return s.keepUnknown(ctx, prepared, domain.IntentErrorOutcomeUnknown, err)
	}
	return confirmed, nil
}

func (s *intentService) Recover(ctx context.Context, accountID string, intentID int64) (domain.IntentSnapshot, error) {
	current, err := s.store.ByID(ctx, accountID, intentID)
	if err != nil {
		return domain.IntentSnapshot{}, err
	}
	if current.State == domain.OrderStateIntent {
		// No dispatch was prepared. The generic repository keeps this state;
		// the explicit shared-database L1 store may prove its deadline closed.
		return s.store.Unknown(ctx, current, domain.IntentErrorCoreNotFound)
	}
	if current.State == domain.OrderStateRejected || current.State == domain.OrderStateFilled || current.State == domain.OrderStateCanceled {
		return current, nil
	}
	order, err := s.gateway.Find(ctx, accountID, current.ClientOrderID)
	if err != nil {
		code := domain.IntentErrorOutcomeUnknown
		if errors.Is(err, domain.ErrCoreNotFound) {
			code = domain.IntentErrorCoreNotFound
		}
		return s.keepUnknown(ctx, current, code, err)
	}
	confirmed, err := s.store.Confirm(ctx, current, order)
	if err != nil {
		if errors.Is(err, domain.ErrIdempotencyConflict) {
			return s.store.ByID(ctx, accountID, intentID)
		}
		return s.keepUnknown(ctx, current, domain.IntentErrorOutcomeUnknown, err)
	}
	return confirmed, nil
}

func (s *intentService) keepUnknown(ctx context.Context, current domain.IntentSnapshot, code string, cause error) (domain.IntentSnapshot, error) {
	unknown, err := s.store.Unknown(ctx, current, code)
	if err != nil {
		if errors.Is(err, domain.ErrIdempotencyConflict) {
			return s.store.ByID(ctx, current.Input.AccountID, current.ID)
		}
		return current, &unknownOutcome{cause: errors.Join(cause, err)}
	}
	return unknown, nil
}

func confirmedRejection(err error) bool {
	return errors.Is(err, domain.ErrInsufficientFunds) || errors.Is(err, domain.ErrRiskDenied) ||
		errors.Is(err, domain.ErrLeaseLost) || errors.Is(err, domain.ErrInvalidOrder) ||
		errors.Is(err, domain.ErrUnsupportedOrder) || errors.Is(err, domain.ErrSelfTrade)
}
