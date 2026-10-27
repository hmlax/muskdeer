package usecase

import (
	"context"
	"errors"

	"github.com/hmlax/muskdeer/internal/domain"
)

type cancellationService struct {
	requests domain.CancellationRepository
	intents  domain.IntentExecutionRepository
	gateway  domain.TradingGateway
	recovery domain.IntentService
}

// NewCancellationService owns only business coordination, with borrowed stores
// and gateway. Core observations drive release through the intent repository.
func NewCancellationService(requests domain.CancellationRepository, intents domain.IntentExecutionRepository, gateway domain.TradingGateway) (domain.CancellationService, error) {
	if requests == nil || intents == nil || gateway == nil {
		return nil, domain.ErrInvalidOrder
	}
	recovery, err := NewIntentService(intents, gateway)
	if err != nil {
		return nil, err
	}
	return &cancellationService{
		requests: requests,
		intents:  intents,
		gateway:  gateway,
		recovery: recovery,
	}, nil
}

func (s *cancellationService) Cancel(ctx context.Context, input domain.CancelInput, execution domain.LeaseToken) (domain.CancellationResult, error) {
	request, err := s.requests.Accept(ctx, input, execution)
	if err != nil {
		return domain.CancellationResult{}, err
	}
	if request.State != domain.CancellationRequested {
		return s.Recover(ctx, input.AccountID, request.ID)
	}
	parent, err := s.recovery.Recover(ctx, input.AccountID, request.IntentID)
	if err != nil {
		return domain.CancellationResult{}, err
	}
	if terminalOrder(parent.State) || parent.CancelState == domain.CancelStatePending || parent.CoreOrderID == 0 {
		return s.observeOrder(ctx, request, parent)
	}
	prepared, err := s.requests.Prepare(ctx, request)
	if err != nil {
		if errors.Is(err, domain.ErrIdempotencyConflict) || errors.Is(err, domain.ErrOutcomeUnknown) {
			return s.Recover(ctx, input.AccountID, request.ID)
		}
		return domain.CancellationResult{}, err
	}
	coreInput := prepared.Input
	coreInput.IdempotencyKey = prepared.CoreCommandKey
	attemptCtx, stop := context.WithDeadline(ctx, coreInput.Deadline)
	defer stop()
	order, err := s.gateway.Cancel(attemptCtx, domain.GatewayCancel{
		IntentID:           parent.ID,
		Input:              coreInput,
		ExecutionAuthority: prepared.ExecutionAuthority,
	})
	if err != nil {
		state, code := domain.CancellationUnknown, domain.IntentErrorOutcomeUnknown
		if confirmedRejection(err) {
			state, code = domain.CancellationRejected, domain.IntentErrorCoreRejected
		}
		observed, saveErr := s.requests.Observe(ctx, prepared, state, code)
		if saveErr != nil {
			if errors.Is(saveErr, domain.ErrIdempotencyConflict) {
				return s.Recover(ctx, input.AccountID, request.ID)
			}
			return domain.CancellationResult{}, &unknownOutcome{cause: errors.Join(err, saveErr)}
		}
		return domain.CancellationResult{
			Request: observed,
			Order:   parent,
		}, nil
	}
	confirmed, err := s.intents.Confirm(ctx, parent, order)
	if err != nil {
		if errors.Is(err, domain.ErrIdempotencyConflict) {
			return s.Recover(ctx, input.AccountID, request.ID)
		}
		return domain.CancellationResult{}, &unknownOutcome{cause: err}
	}
	return s.observeOrder(ctx, prepared, confirmed)
}

func (s *cancellationService) Recover(ctx context.Context, accountID string, id int64) (domain.CancellationResult, error) {
	request, err := s.requests.ByID(ctx, accountID, id)
	if err != nil {
		return domain.CancellationResult{}, err
	}
	return s.recoverRequest(ctx, request)
}

func (s *cancellationService) RecoverByKey(ctx context.Context, accountID, idempotencyKey string) (domain.CancellationResult, error) {
	request, err := s.requests.ByKey(ctx, accountID, idempotencyKey)
	if err != nil {
		return domain.CancellationResult{}, err
	}
	return s.recoverRequest(ctx, request)
}

func (s *cancellationService) recoverRequest(ctx context.Context, request domain.CancellationSnapshot) (domain.CancellationResult, error) {
	parent, err := s.recovery.Recover(ctx, request.Input.AccountID, request.IntentID)
	if err != nil {
		return domain.CancellationResult{}, err
	}
	return s.observeOrder(ctx, request, parent)
}

func (s *cancellationService) observeOrder(ctx context.Context, request domain.CancellationSnapshot, parent domain.IntentSnapshot) (domain.CancellationResult, error) {
	state, code := domain.CancellationUnknown, domain.IntentErrorOutcomeUnknown
	switch {
	case terminalOrder(parent.State):
		state, code = domain.CancellationConfirmed, ""
	case parent.CancelState == domain.CancelStatePending:
		state, code = domain.CancellationPending, ""
	case request.State == domain.CancellationRequested:
		state, code = domain.CancellationRequested, domain.IntentErrorCoreNotFound
	}
	observed, err := s.requests.Observe(ctx, request, state, code)
	if err != nil {
		if errors.Is(err, domain.ErrIdempotencyConflict) {
			latest, loadErr := s.requests.ByID(ctx, request.Input.AccountID, request.ID)
			if loadErr != nil {
				return domain.CancellationResult{}, loadErr
			}
			return domain.CancellationResult{
				Request: latest,
				Order:   parent,
			}, nil
		}
		return domain.CancellationResult{}, err
	}
	return domain.CancellationResult{
		Request: observed,
		Order:   parent,
	}, nil
}

func terminalOrder(state domain.OrderState) bool {
	return state == domain.OrderStateFilled || state == domain.OrderStateCanceled || state == domain.OrderStateRejected
}
