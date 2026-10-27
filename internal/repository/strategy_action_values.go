package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/hmlax/muskdeer/deer/ownerid"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

const strategyActionRPCPrefix = "strategy-action."

func validateActionPlan(ctx context.Context, plan domain.StrategyActionPlan) error {
	if err := coreContext(ctx); err != nil {
		return err
	}
	return actionPlanFields(plan)
}

func actionPlanFields(plan domain.StrategyActionPlan) error {
	if !coreIdentifier(plan.Key, 128) || !coreIdentifier(plan.AccountID, 64) || plan.InstrumentID <= 0 {
		return domain.ErrInvalidOrder
	}
	switch plan.Kind {
	case domain.StrategyActionSubmit:
		if plan.TargetIntentID != 0 || plan.TargetClientOrderID != "" ||
			!domain.FixedDecimal(plan.Price, 20, 18, false) || !domain.FixedDecimal(plan.Quantity, 20, 18, false) ||
			plan.Side != domain.SideBuy && plan.Side != domain.SideSell ||
			plan.TimeInForce != domain.TimeInForceGTC && plan.TimeInForce != domain.TimeInForcePostOnly || plan.ReduceOnly && plan.Side != domain.SideSell {
			return domain.ErrInvalidOrder
		}
	case domain.StrategyActionCancel:
		if plan.TargetIntentID <= 0 || !coreIdentifier(plan.TargetClientOrderID, 64) || plan.Side != domain.SideUnspecified ||
			!domain.FixedDecimal(plan.Price, 20, 18, true) || !domain.FixedDecimal(plan.Quantity, 20, 18, true) ||
			!plan.Price.IsZero() || !plan.Quantity.IsZero() || plan.TimeInForce != domain.TimeInForceUnspecified || plan.ReduceOnly {
			return domain.ErrInvalidOrder
		}
	default:
		return domain.ErrInvalidOrder
	}
	return nil
}

func actionRPCKey(strategyID int64, decisionKey string) string {
	digest := sha256.Sum256([]byte(strconv.FormatInt(strategyID, 10) + "\n" + decisionKey))
	return strategyActionRPCPrefix + hex.EncodeToString(digest[:])
}

func newActionRow(authority domain.StrategyAuthority, plan domain.StrategyActionPlan) (*modelv1.StrategyAction, error) {
	row := &modelv1.StrategyAction{
		StrategyInstanceId: authority.StrategyID,
		DecisionKey:        plan.Key,
		AccountId:          plan.AccountID,
		InstrumentId:       plan.InstrumentID,
		ConfigVersion:      authority.ConfigVersion,
		Kind:               string(plan.Kind),
		ReduceOnly:         plan.ReduceOnly,
		IdempotencyKey:     actionRPCKey(authority.StrategyID, plan.Key),
		State:              string(domain.StrategyActionPending),
		OwnerId:            authority.OwnerID,
		OwnerEpoch:         authority.OwnerEpoch,
		Revision:           1,
	}
	if plan.Kind == domain.StrategyActionCancel {
		row.IntentId = proto.Int64(plan.TargetIntentID)
		row.ClientOrderId = plan.TargetClientOrderID
		return row, nil
	}
	var err error
	row.ClientOrderId, err = domain.IntentClientOrderID(plan.AccountID, row.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	row.Side = proto.String(string(plan.Side))
	row.Price, row.Quantity = proto.String(plan.Price.String()), proto.String(plan.Quantity.String())
	row.TimeInForce = proto.String(string(plan.TimeInForce))
	return row, nil
}

func actionSnapshot(row *modelv1.StrategyAction) (domain.StrategyAction, error) {
	if row == nil || row.Id <= 0 || row.DeleteAt != nil || row.Revision <= 0 || row.ConfigVersion <= 0 || row.OwnerEpoch <= 0 || !ownerid.Valid(row.OwnerId) ||
		row.CreateAt <= 0 || row.UpdateAt < row.CreateAt || row.IdempotencyKey != actionRPCKey(row.StrategyInstanceId, row.DecisionKey) {
		return domain.StrategyAction{}, domain.ErrInvalidOrder
	}
	plan := domain.StrategyActionPlan{
		Key:          row.DecisionKey,
		Kind:         domain.StrategyActionKind(row.Kind),
		AccountID:    row.AccountId,
		InstrumentID: row.InstrumentId,
		ReduceOnly:   row.ReduceOnly,
	}
	if plan.Kind == domain.StrategyActionCancel {
		plan.TargetIntentID, plan.TargetClientOrderID = row.GetIntentId(), row.ClientOrderId
	} else {
		var err error
		rule := domain.DecimalRule{
			IntegerDigits: 20,
			Scale:         18,
		}
		plan.Price, err = domain.ParseDecimal(row.GetPrice(), rule)
		if err != nil {
			return domain.StrategyAction{}, err
		}
		plan.Quantity, err = domain.ParseDecimal(row.GetQuantity(), rule)
		if err != nil {
			return domain.StrategyAction{}, err
		}
		plan.Side, plan.TimeInForce = domain.Side(row.GetSide()), domain.TimeInForce(row.GetTimeInForce())
		clientID, err := domain.IntentClientOrderID(row.AccountId, row.IdempotencyKey)
		if err != nil || clientID != row.ClientOrderId {
			return domain.StrategyAction{}, domain.ErrInvalidOrder
		}
	}
	if err := actionPlanFields(plan); err != nil {
		return domain.StrategyAction{}, err
	}
	result := domain.StrategyAction{
		ID:                        row.Id,
		StrategyID:                row.StrategyInstanceId,
		ConfigVersion:             row.ConfigVersion,
		Plan:                      plan,
		IdempotencyKey:            row.IdempotencyKey,
		ClientOrderID:             row.ClientOrderId,
		State:                     domain.StrategyActionState(row.State),
		IntentID:                  row.GetIntentId(),
		CancellationID:            row.GetCancellationId(),
		ObservedOrderState:        domain.OrderState(row.ObservedOrderState),
		ObservedCancellationState: domain.CancellationState(row.ObservedCancellationState),
		OwnerID:                   row.OwnerId,
		OwnerEpoch:                row.OwnerEpoch,
		Revision:                  row.Revision,
		CreatedAt:                 time.UnixMilli(row.CreateAt).UTC(),
		UpdatedAt:                 time.UnixMilli(row.UpdateAt).UTC(),
	}
	if row.DeadlineAt > 0 {
		result.Deadline = time.UnixMilli(row.DeadlineAt).UTC()
	}
	return result, nil
}

func sameActionPlan(old domain.StrategyAction, plan domain.StrategyActionPlan, version int64) bool {
	p := old.Plan
	return old.ConfigVersion == version && p.Kind == plan.Kind && p.AccountID == plan.AccountID && p.InstrumentID == plan.InstrumentID &&
		p.Side == plan.Side && p.Price.Equal(plan.Price) && p.Quantity.Equal(plan.Quantity) && p.TimeInForce == plan.TimeInForce && p.ReduceOnly == plan.ReduceOnly &&
		p.TargetIntentID == plan.TargetIntentID && p.TargetClientOrderID == plan.TargetClientOrderID
}

func lockActionAuthority(ctx context.Context, tx *gorm.DB, authority domain.StrategyAuthority) (*modelv1.StrategyInstance, int64, error) {
	if err := coreContext(ctx); err != nil {
		return nil, 0, err
	}
	if err := actionAuthorityFields(authority); err != nil {
		return nil, 0, err
	}
	var strategy modelv1.StrategyInstance
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).
		Where("id = ? AND delete_at IS NULL", authority.StrategyID).Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).First(&strategy).Error; err != nil {
		return nil, 0, err
	}
	if strategy.ActiveConfigVersion == nil || *strategy.ActiveConfigVersion != authority.ConfigVersion {
		return nil, 0, domain.ErrLeaseLost
	}
	lease, now, err := lockLease(ctx, tx, domain.LeaseKey{
		Kind:       domain.LeaseKindStrategy,
		StrategyID: authority.StrategyID,
	})
	if err != nil {
		return nil, 0, err
	}
	if lease.GetOwnerId() != authority.OwnerID || lease.GetEpoch() != authority.OwnerEpoch || lease.GetExpiresAt() <= now {
		return nil, 0, domain.ErrLeaseLost
	}
	return &strategy, now, nil
}

func actionAuthorityFields(authority domain.StrategyAuthority) error {
	if authority.StrategyID <= 0 || authority.ConfigVersion <= 0 || authority.OwnerEpoch <= 0 || !ownerid.Valid(authority.OwnerID) {
		return domain.ErrInvalidOrder
	}
	return nil
}

func activeActionStates() []domain.StrategyActionState {
	return []domain.StrategyActionState{domain.StrategyActionPending, domain.StrategyActionDispatching, domain.StrategyActionWaiting, domain.StrategyActionUnknown}
}

func actionFailure(ctx context.Context, err error) error {
	if errors.Is(err, domain.ErrOutcomeUnknown) {
		return err
	}
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) {
		var category error
		switch databaseError.ConstraintName {
		case "strategy_action_fence":
			category = domain.ErrLeaseLost
		case "strategy_action_unresolved", "uk_strategy_action_active_submit", "uk_strategy_action_active_cancel":
			category = domain.ErrOutcomeUnknown
		case "strategy_action_identity", "ck_strategy_action_payload", "ck_strategy_action_deadline", "ck_strategy_action_observed":
			category = domain.ErrInvalidOrder
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
