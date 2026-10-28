package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type strategyLifecycle struct{ db *gorm.DB }

// NewStrategyLifecycle borrows root storage without claiming an instance,
// initializing funds, or granting permission to trade.
func NewStrategyLifecycle(db *gorm.DB) (domain.StrategyLifecycle, error) {
	if db == nil {
		return nil, domain.ErrInvalidOrder
	}
	return &strategyLifecycle{db: db}, nil
}

func (s *strategyLifecycle) Instance(ctx context.Context, strategyID int64) (domain.StrategySpec, error) {
	if err := coreContext(ctx); err != nil {
		return domain.StrategySpec{}, err
	}
	if strategyID <= 0 {
		return domain.StrategySpec{}, domain.ErrInvalidOrder
	}
	var row modelv1.StrategyInstance
	if err := s.db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).
		Where("id = ? AND delete_at IS NULL", strategyID).First(&row).Error; err != nil {
		return domain.StrategySpec{}, lifecycleFailure(ctx, err)
	}
	return strategySpec(&row), nil
}

func (s *strategyLifecycle) Transition(ctx context.Context, current domain.StrategySpec, token domain.LeaseToken, transition domain.StrategyTransition) (domain.StrategySpec, error) {
	if err := lifecycleBoundary(ctx, current.ID, token, transition); err != nil {
		return domain.StrategySpec{}, err
	}
	if current.Revision <= 0 {
		return domain.StrategySpec{}, domain.ErrInvalidOrder
	}
	var result domain.StrategySpec
	err := s.db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
		row, err := lockLifecycle(ctx, tx, current, token)
		if err != nil {
			return err
		}
		if err := lifecycleDesired(row, transition); err != nil {
			return err
		}
		if lifecycleUnchanged(row, token, transition) {
			result = strategySpec(row)
			return nil
		}
		if err := prepareLifecycle(ctx, tx, row, token, transition); err != nil {
			return err
		}
		previous := row.Revision
		row.State, row.RuntimeOwnerId, row.RuntimeOwnerEpoch = string(transition.State), token.OwnerID, token.Epoch
		row.Revision++
		updated := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ? AND revision = ?", row.Id, previous).
			Select("State", "ActiveConfigVersion", "ActiveMarketDataSource", "RuntimeOwnerId", "RuntimeOwnerEpoch", "Revision").Updates(row)
		if err := reservationResult(updated); err != nil {
			return err
		}
		if transition.ActivateRequested {
			if err := validateLifecycleInventory(ctx, tx, row); err != nil {
				return err
			}
		}
		if err := invalidateLifecycleRisk(ctx, tx, row); err != nil {
			return err
		}
		result = strategySpec(row)
		return nil
	})
	if err != nil {
		return domain.StrategySpec{}, lifecycleFailure(ctx, err)
	}
	return result, nil
}

func lockLifecycle(ctx context.Context, tx *gorm.DB, current domain.StrategySpec, token domain.LeaseToken) (*modelv1.StrategyInstance, error) {
	var row modelv1.StrategyInstance
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ? AND delete_at IS NULL", current.ID).
		Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).First(&row).Error; err != nil {
		return nil, err
	}
	if row.Revision != current.Revision {
		return nil, domain.ErrIdempotencyConflict
	}
	lease, now, err := lockLease(ctx, tx, token.Key)
	if err != nil {
		return nil, err
	}
	if lease.GetOwnerId() != token.OwnerID || lease.GetEpoch() != token.Epoch || lease.GetExpiresAt() <= now || row.RuntimeOwnerEpoch > token.Epoch {
		return nil, domain.ErrLeaseLost
	}
	return &row, nil
}

func prepareLifecycle(ctx context.Context, tx *gorm.DB, row *modelv1.StrategyInstance, token domain.LeaseToken, transition domain.StrategyTransition) error {
	if transition.State == domain.StrategyStateRunning &&
		(row.RuntimeOwnerId != token.OwnerID || row.RuntimeOwnerEpoch != token.Epoch || row.State != string(domain.StrategyStateRecovering) && row.State != string(domain.StrategyStateStarting)) {
		return domain.ErrOutcomeUnknown
	}
	if transition.State == domain.StrategyStateRunning || transition.State == domain.StrategyStateStopped || transition.State == domain.StrategyStatePaused || transition.ActivateRequested {
		if err := requireStrategyClear(ctx, tx, row.Id); err != nil {
			return err
		}
	}
	if transition.ActivateRequested {
		row.ActiveConfigVersion, row.ActiveMarketDataSource = proto.Int64(row.RequestedConfigVersion), proto.String(row.MarketDataSource)
		if _, err := riskParameterAge(ctx, tx, row); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrInvalidParameters
			}
			return err
		}
	}
	if transition.State != domain.StrategyStateRunning {
		return nil
	}
	if row.ActiveConfigVersion == nil || row.ActiveMarketDataSource == nil {
		return domain.ErrInvalidParameters
	}
	decision, age, err := evaluateRiskInputs(ctx, tx, row)
	if err != nil {
		return err
	}
	now, err := riskEvaluationTime(ctx, tx, row.Id)
	if err != nil {
		return err
	}
	if decision.Reason != domain.RiskReasonReady || !riskGrantExpiry(now, decision.Market, time.Duration(age)*time.Millisecond).After(now) {
		return domain.ErrRiskDenied
	}
	return nil
}

func requireStrategyClear(ctx context.Context, tx *gorm.DB, strategyID int64) error {
	queries := []*gorm.DB{
		tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).Where("strategy_instance_id = ? AND delete_at IS NULL AND state IN ?", strategyID,
			[]domain.OrderState{domain.OrderStateIntent, domain.OrderStateSubmitting, domain.OrderStateLive, domain.OrderStatePartiallyFilled, domain.OrderStateUnknown}),
		tx.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Where("strategy_instance_id = ? AND delete_at IS NULL AND state IN ?", strategyID,
			[]domain.OrderState{domain.OrderStateLive, domain.OrderStatePartiallyFilled}),
		tx.WithContext(ctx).Model(&modelv1.StrategyAction{}).Where("strategy_instance_id = ? AND delete_at IS NULL AND state IN ?", strategyID, activeActionStates()),
	}
	for _, query := range queries {
		var count int64
		if err := query.Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return domain.ErrOutcomeUnknown
		}
	}
	return nil
}

func validateLifecycleInventory(ctx context.Context, tx *gorm.DB, row *modelv1.StrategyInstance) error {
	core, err := lockCoreRows(ctx, tx, row.AccountId, row.InstrumentId)
	if err != nil {
		return err
	}
	var budget modelv1.AccountBudget
	if err := tx.WithContext(ctx).Model(&modelv1.AccountBudget{}).Where("account_id = ? AND delete_at IS NULL", row.AccountId).
		Clauses(clause.Locking{Strength: "UPDATE"}).First(&budget).Error; err != nil {
		return err
	}
	return validateWorstInventory(ctx, tx, row.AccountId, core.instrument, decimal.Zero)
}

func invalidateLifecycleRisk(ctx context.Context, tx *gorm.DB, row *modelv1.StrategyInstance) error {
	var risk modelv1.RiskSnapshot
	err := tx.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ? AND delete_at IS NULL", row.Id).
		Clauses(clause.Locking{Strength: "UPDATE"}).First(&risk).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	previous := risk.Revision
	risk.AllowNewRisk = false
	risk.ReasonCode = string(domain.RiskReasonNotEvaluated)
	risk.Revision++
	updated := tx.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("id = ? AND revision = ?", risk.Id, previous).
		Select("AllowNewRisk", "ReasonCode", "Revision").Updates(&risk)
	return reservationResult(updated)
}

func lifecycleFailure(ctx context.Context, err error) error {
	if errors.Is(err, domain.ErrOutcomeUnknown) || errors.Is(err, domain.ErrInvalidParameters) {
		return err
	}
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) {
		var category error
		switch databaseError.ConstraintName {
		case "strategy_runtime_fence":
			category = domain.ErrLeaseLost
		case "strategy_runtime_transition", "strategy_runtime_unresolved":
			category = domain.ErrOutcomeUnknown
		}
		if category != nil {
			return &corePersistenceError{
				category: category,
				cause:    errors.Join(err, ctx.Err()),
			}
		}
	}
	return coreFailure(ctx, err)
}
