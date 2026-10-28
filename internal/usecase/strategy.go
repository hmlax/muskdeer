package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/hmlax/muskdeer/deer/ownerid"
	"github.com/hmlax/muskdeer/internal/domain"
)

// StrategyDependencies are borrowed business ports. No constructor opens a
// resource, spawns work, initializes funds or grants trading permissions.
type StrategyDependencies struct {
	Leases      domain.LeaseRepository
	Lifecycle   domain.StrategyLifecycle
	Environment domain.StrategyEnvironment
	Actions     domain.StrategyActions
	Execution   domain.StrategyExecution
	Market      domain.MarketReader
}

// StrategySettings binds one instance to a fresh process incarnation.
type StrategySettings struct {
	StrategyID int64
	OwnerID    string
	LeaseTTL   time.Duration
}

// StrategyController is called serially by its owning Deer worker. Its cursor
// only schedules observations; all effect authority and uncertainty are durable.
type StrategyController struct {
	deps        StrategyDependencies
	settings    StrategySettings
	afterAction int64
}

const messageStrategyReobserve = "strategy state observation changed"

var errStrategyReobserve = errors.New(messageStrategyReobserve)

// NewStrategyController constructs one bounded, serial strategy loop body.
func NewStrategyController(deps StrategyDependencies, settings StrategySettings) (*StrategyController, error) {
	if deps.Leases == nil || deps.Lifecycle == nil || deps.Environment == nil || deps.Actions == nil || deps.Execution == nil || deps.Market == nil ||
		settings.StrategyID <= 0 || !ownerid.Valid(settings.OwnerID) || settings.LeaseTTL < domain.MinLeaseTTL || settings.LeaseTTL > domain.MaxLeaseTTL || settings.LeaseTTL%time.Millisecond != 0 {
		return nil, domain.ErrInvalidOrder
	}
	return &StrategyController{
		deps:     deps,
		settings: settings,
	}, nil
}

// Tick performs bounded recovery or one new command. It always requests the
// configured poll delay, including after a command; it never busy-retries I/O.
func (s *StrategyController) Tick(ctx context.Context) (bool, error) {
	worked, err := s.tick(ctx)
	if errors.Is(err, errStrategyReobserve) {
		return false, nil
	}
	return worked, err
}

func (s *StrategyController) tick(ctx context.Context) (worked bool, resultErr error) {
	stage := domain.StrategyStageBoundary
	defer func() {
		if resultErr != nil {
			resultErr = &domain.StrategyFailure{
				Stage: stage,
				Cause: resultErr,
			}
		}
	}()
	if ctx == nil {
		return false, domain.ErrInvalidOrder
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	deadline, bounded := ctx.Deadline()
	if !bounded || time.Until(deadline) > domain.MaxIntentLifetime || time.Until(deadline) >= s.settings.LeaseTTL/2 {
		return false, domain.ErrInvalidOrder
	}
	key := domain.LeaseKey{
		Kind:       domain.LeaseKindStrategy,
		StrategyID: s.settings.StrategyID,
	}
	stage = domain.StrategyStageAcquire
	token, err := s.deps.Leases.Acquire(ctx, key, s.settings.OwnerID, s.settings.LeaseTTL)
	if errors.Is(err, domain.ErrLeaseHeld) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	stage = domain.StrategyStageRenew
	token, err = s.deps.Leases.Renew(ctx, token, s.settings.LeaseTTL)
	if err != nil {
		return false, err
	}
	stage = domain.StrategyStageInstance
	current, err := s.deps.Lifecycle.Instance(ctx, key.StrategyID)
	if err != nil {
		return false, err
	}
	if current.RuntimeOwnerID != token.OwnerID || current.RuntimeOwnerEpoch != token.Epoch {
		stage = domain.StrategyStageClaim
		_, err := s.transition(ctx, current, token, domain.StrategyStateRecovering, false)
		return false, err
	}
	authority := strategyAuthority(current, token)
	stage = domain.StrategyStageActions
	handled, err := s.recoverActions(ctx, current, authority)
	if err != nil || handled {
		return false, err
	}
	stage = domain.StrategyStageOrders
	orders, err := s.currentOrders(ctx, current)
	if err != nil {
		return false, err
	}
	if !current.DesiredRunning {
		stage = domain.StrategyStageStop
		return false, s.stop(ctx, current, token, orders)
	}
	if current.State != domain.StrategyStateRunning || current.ActiveVersion != current.RequestedVersion || current.ActiveSource != current.RequestedSource {
		stage = domain.StrategyStageRecover
		return false, s.recover(ctx, current, token, orders)
	}
	stage = domain.StrategyStageQuote
	return false, s.quote(ctx, current, token, orders)
}

func (s *StrategyController) transition(ctx context.Context, current domain.StrategySpec, token domain.LeaseToken, state domain.StrategyState, activate bool) (domain.StrategySpec, error) {
	result, err := s.deps.Lifecycle.Transition(ctx, current, token, domain.StrategyTransition{
		State:             state,
		ActivateRequested: activate,
	})
	if errors.Is(err, domain.ErrIdempotencyConflict) {
		return domain.StrategySpec{}, errStrategyReobserve
	}
	return result, localStrategyFailure(ctx, err)
}

func (s *StrategyController) recover(ctx context.Context, current domain.StrategySpec, token domain.LeaseToken, orders []domain.IntentSnapshot) error {
	current, err := s.transition(ctx, current, token, domain.StrategyStateRecovering, false)
	if err != nil {
		return err
	}
	if len(orders) != 0 {
		return s.protect(ctx, current, token, orders)
	}
	active, err := s.deps.Actions.Active(ctx, current.ID)
	if err != nil || len(active) != 0 {
		return localStrategyFailure(ctx, err)
	}
	_, err = s.transition(ctx, current, token, domain.StrategyStateRunning, true)
	if errors.Is(err, domain.ErrRiskDenied) || errors.Is(err, domain.ErrInvalidParameters) || errors.Is(err, domain.ErrOutcomeUnknown) {
		// A complete healthy selected source and reconciliation are prerequisites,
		// never synthesized by waiting or by another source being available.
		return nil
	}
	return err
}

func (s *StrategyController) stop(ctx context.Context, current domain.StrategySpec, token domain.LeaseToken, orders []domain.IntentSnapshot) error {
	target, pending := domain.StrategyStateStopped, domain.StrategyStateStopping
	if current.DesiredState == domain.StrategyStatePaused {
		target, pending = domain.StrategyStatePaused, domain.StrategyStatePausing
	} else if current.DesiredState != domain.StrategyStateStopped {
		return domain.ErrInvalidParameters
	}
	if current.State != target {
		var err error
		current, err = s.transition(ctx, current, token, pending, false)
		if err != nil {
			return err
		}
	}
	if len(orders) != 0 {
		return s.protect(ctx, current, token, orders)
	}
	_, err := s.transition(ctx, current, token, target, false)
	if errors.Is(err, domain.ErrOutcomeUnknown) {
		return nil
	}
	return err
}

func strategyAuthority(spec domain.StrategySpec, token domain.LeaseToken) domain.StrategyAuthority {
	return domain.StrategyAuthority{
		StrategyID:    spec.ID,
		ConfigVersion: spec.ActiveVersion,
		OwnerID:       token.OwnerID,
		OwnerEpoch:    token.Epoch,
	}
}
