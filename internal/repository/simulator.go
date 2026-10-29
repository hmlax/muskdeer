package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/hmlax/muskdeer/deer/ownerid"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type simulator struct{ db *gorm.DB }

type coreRows struct {
	instrument *modelv1.Instrument
	account    *modelv1.SimulatorAccount
}

type corePersistenceError struct {
	cause    error
	category error
}

func (e *corePersistenceError) Error() string {
	if e.category != nil {
		return e.category.Error()
	}
	return domain.MessageCoreUnavailable
}

func (e *corePersistenceError) Unwrap() error { return errors.Join(e.category, e.cause) }

// NewSimulator borrows a Deer database. It never seeds funds, migrates, opens a
// connection, or starts background work. Scenario controls need trusted callers.
func NewSimulator(db *gorm.DB) (domain.Simulator, error) {
	if db == nil {
		return nil, domain.ErrInvalidOrder
	}
	return &simulator{db: db}, nil
}

func (s *simulator) Find(ctx context.Context, accountID, clientOrderID string) (domain.CoreOrder, error) {
	if err := coreContext(ctx); err != nil {
		return domain.CoreOrder{}, err
	}
	if !coreIdentifier(accountID, 64) || !coreIdentifier(clientOrderID, 64) {
		return domain.CoreOrder{}, domain.ErrInvalidOrder
	}
	var row modelv1.SimulatorOrder
	err := s.db.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Where("account_id = ? AND client_order_id = ? AND delete_at IS NULL", accountID, clientOrderID).First(&row).Error
	if err != nil {
		return domain.CoreOrder{}, coreFailure(ctx, err)
	}
	return coreOrder(&row)
}

func (s *simulator) Balances(ctx context.Context, accountID string) ([]domain.CoreBalance, error) {
	if err := coreContext(ctx); err != nil {
		return nil, err
	}
	if !coreIdentifier(accountID, 64) {
		return nil, domain.ErrInvalidOrder
	}
	var rows []*modelv1.SimulatorBalance
	if err := s.db.WithContext(ctx).Model(&modelv1.SimulatorBalance{}).Where("account_id = ? AND delete_at IS NULL", accountID).Order("asset ASC").Find(&rows).Error; err != nil {
		return nil, coreFailure(ctx, err)
	}
	if len(rows) == 0 {
		return nil, domain.ErrCoreNotFound
	}
	result := make([]domain.CoreBalance, 0, len(rows))
	for _, row := range rows {
		available, err := decimal.NewFromString(row.Available)
		if err != nil {
			return nil, coreFailure(ctx, err)
		}
		frozen, err := decimal.NewFromString(row.Frozen)
		if err != nil {
			return nil, coreFailure(ctx, err)
		}
		result = append(result, domain.CoreBalance{
			Asset:     row.Asset,
			Available: available,
			Frozen:    frozen,
		})
	}
	return result, nil
}

func coreContext(ctx context.Context) error {
	if ctx == nil {
		return domain.ErrInvalidOrder
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return domain.ErrInvalidOrder
	}
	return ctx.Err()
}

func coreIdentifier(value string, maximum int) bool {
	if len(value) == 0 || len(value) > maximum {
		return false
	}
	for index := range len(value) {
		c := value[index]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == ':' {
			continue
		}
		return false
	}
	return true
}

func validateCoreCommand(ctx context.Context, authority domain.StrategyAuthority, execution domain.LeaseToken, accountID, clientID, key string, instrumentID int64, deadline time.Time) error {
	if err := coreContext(ctx); err != nil {
		return err
	}
	limit, _ := ctx.Deadline()
	now := time.Now()
	if !coreIdentifier(accountID, 64) || !coreIdentifier(clientID, 64) || !coreIdentifier(key, 128) ||
		instrumentID <= 0 || authority.StrategyID <= 0 || authority.ConfigVersion <= 0 || authority.OwnerEpoch <= 0 || !ownerid.Valid(authority.OwnerID) ||
		execution.Key.Kind != domain.LeaseKindExecution || execution.Key.AccountID != accountID || execution.Key.StrategyID != 0 || execution.Key.MarketFeedID != 0 || execution.Epoch <= 0 || !ownerid.Valid(execution.OwnerID) ||
		!deadline.After(now) || deadline.After(limit) || deadline.Sub(now) > domain.MaxIntentLifetime {
		return domain.ErrInvalidOrder
	}
	return nil
}

func lockGateway(ctx context.Context, tx *gorm.DB, authority domain.StrategyAuthority, execution domain.LeaseToken, accountID string, instrumentID int64) (coreRows, error) {
	if _, _, err := lockAuthority(ctx, tx, authority, execution, accountID, instrumentID); err != nil {
		return coreRows{}, err
	}
	return lockCoreRows(ctx, tx, accountID, instrumentID)
}

func lockAuthority(ctx context.Context, tx *gorm.DB, authority domain.StrategyAuthority, execution domain.LeaseToken, accountID string, instrumentID int64) (*modelv1.StrategyInstance, int64, error) {
	var strategy modelv1.StrategyInstance
	// Admission, action resolution and the final core gate use one mutex.
	// Acquire it before either lease: upgrading SHARE after taking a lease
	// deadlocks with another admission holding SHARE while waiting for it.
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("delete_at IS NULL").Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).First(&strategy, authority.StrategyID).Error; err != nil {
		return nil, 0, err
	}
	if strategy.AccountId != accountID || strategy.InstrumentId != instrumentID || strategy.ActiveConfigVersion == nil || *strategy.ActiveConfigVersion != authority.ConfigVersion {
		return nil, 0, domain.ErrLeaseLost
	}
	strategyKey := domain.LeaseKey{
		Kind:       domain.LeaseKindStrategy,
		StrategyID: authority.StrategyID,
	}
	strategyLease, now, err := lockLease(ctx, tx, strategyKey)
	if err != nil {
		return nil, 0, err
	}
	if strategyLease.GetOwnerId() != authority.OwnerID || strategyLease.GetEpoch() != authority.OwnerEpoch || strategyLease.GetExpiresAt() <= now {
		return nil, 0, domain.ErrLeaseLost
	}
	executionLease, now, err := lockLease(ctx, tx, execution.Key)
	if err != nil {
		return nil, 0, err
	}
	if executionLease.GetOwnerId() != execution.OwnerID || executionLease.GetEpoch() != execution.Epoch || executionLease.GetExpiresAt() <= now {
		return nil, 0, domain.ErrLeaseLost
	}
	return &strategy, now, nil
}

func lockCoreRows(ctx context.Context, tx *gorm.DB, accountID string, instrumentID int64) (coreRows, error) {
	var instrument modelv1.Instrument
	// Matching needs mutually exclusive writers, without blocking foreign-key
	// KEY SHARE checks for another strategy's durable, unsent action metadata.
	if err := tx.WithContext(ctx).Model(&modelv1.Instrument{}).Where("delete_at IS NULL").Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).First(&instrument, instrumentID).Error; err != nil {
		return coreRows{}, err
	}
	var account modelv1.SimulatorAccount
	if err := tx.WithContext(ctx).Model(&modelv1.SimulatorAccount{}).Where("account_id = ? AND delete_at IS NULL", accountID).Clauses(clause.Locking{Strength: "UPDATE"}).First(&account).Error; err != nil {
		return coreRows{}, err
	}
	return coreRows{
		instrument: &instrument,
		account:    &account,
	}, nil
}

func findCoreOrder(ctx context.Context, tx *gorm.DB, accountID, clientOrderID string) (*modelv1.SimulatorOrder, error) {
	var order modelv1.SimulatorOrder
	err := tx.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Where("account_id = ? AND client_order_id = ? AND delete_at IS NULL", accountID, clientOrderID).
		Clauses(clause.Locking{Strength: "UPDATE"}).First(&order).Error
	return &order, err
}

func saveCoreOrder(ctx context.Context, tx *gorm.DB, row *modelv1.SimulatorOrder) error {
	previous := row.Revision
	row.Revision++
	result := tx.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Where("id = ? AND revision = ?", row.Id, previous).
		Select("State", "CancelState", "FilledQuantity", "FrozenAmount", "LastCommandId", "Revision").Updates(row)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return domain.ErrIdempotencyConflict
	}
	return nil
}

func coreFailure(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	for _, known := range []error{domain.ErrInvalidOrder, domain.ErrLeaseLost, domain.ErrInsufficientFunds, domain.ErrIdempotencyConflict, domain.ErrCoreNotFound, domain.ErrSelfTrade, domain.ErrUnsupportedOrder, domain.ErrRiskDenied, domain.ErrProjectionIncomplete, domain.ErrReconciliationMismatch} {
		if errors.Is(err, known) {
			return err
		}
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ErrCoreNotFound
	}
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) {
		if databaseError.ConstraintName == "strategy_action_not_accepted" {
			return &corePersistenceError{
				category: domain.ErrInvalidOrder,
				cause:    err,
			}
		}
		if databaseError.ConstraintName == "simulator_command_fence" {
			return &corePersistenceError{
				category: domain.ErrLeaseLost,
				cause:    err,
			}
		}
		if databaseError.ConstraintName == "simulator_intent_risk" || databaseError.ConstraintName == "simulator_market_risk" || databaseError.ConstraintName == "oms_global_risk" || databaseError.ConstraintName == "simulator_inventory_risk" || databaseError.ConstraintName == "simulator_configuration_pending" || databaseError.ConstraintName == "simulator_admin_risk" {
			return &corePersistenceError{
				category: domain.ErrRiskDenied,
				cause:    err,
			}
		}
		if databaseError.Code == "23505" {
			return &corePersistenceError{
				category: domain.ErrIdempotencyConflict,
				cause:    err,
			}
		}
	}
	return &corePersistenceError{cause: errors.Join(err, ctx.Err())}
}
