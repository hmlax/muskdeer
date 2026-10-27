package repository

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func stageAcceptedIntent(ctx context.Context, tx *gorm.DB, in domain.OrderInput, execution domain.LeaseToken, strategy *modelv1.StrategyInstance, instrument *modelv1.Instrument, account *modelv1.AccountBudget, clientID, fingerprint string) (*modelv1.OrderIntent, error) {
	var budget modelv1.StrategyBudget
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Where("strategy_instance_id = ? AND account_id = ? AND delete_at IS NULL", strategy.Id, account.AccountId).
		Clauses(clause.Locking{Strength: "UPDATE"}).First(&budget).Error; err != nil {
		return nil, err
	}
	_, now, err := validateRiskSnapshot(ctx, tx, strategy)
	if err != nil {
		return nil, err
	}
	if in.Deadline.UnixMilli() <= now {
		return nil, domain.ErrInvalidOrder
	}
	if err := validateOrderMetadata(in, instrument); err != nil {
		return nil, err
	}
	amount := in.Price.Mul(in.Quantity)
	if !account.Enabled || !budget.Enabled || account.Asset != instrument.QuoteAsset ||
		!domain.FixedDecimal(amount, 40, 36, false) {
		return nil, domain.ErrRiskDenied
	}
	accountReserved, err := reserveWithinLimit(account.ReservedNotional, account.LimitNotional, amount)
	if err != nil {
		return nil, err
	}
	strategyReserved, err := reserveWithinLimit(budget.ReservedNotional, budget.LimitNotional, amount)
	if err != nil {
		return nil, err
	}
	if err := changeGlobalRiskReservation(ctx, tx, instrument.Id, instrument.QuoteAsset, amount); err != nil {
		return nil, err
	}
	if err := saveAccountReservation(ctx, tx, account, accountReserved); err != nil {
		return nil, err
	}
	if err := saveStrategyReservation(ctx, tx, &budget, strategyReserved); err != nil {
		return nil, err
	}
	row := &modelv1.OrderIntent{
		AccountId:             in.AccountID,
		IdempotencyKey:        in.IdempotencyKey,
		ClientOrderId:         clientID,
		StrategyInstanceId:    in.Authority.StrategyID,
		ConfigVersion:         in.Authority.ConfigVersion,
		InstrumentId:          in.InstrumentID,
		RequestId:             in.RequestID,
		Fingerprint:           fingerprint,
		Side:                  string(in.Side),
		Price:                 in.Price.String(),
		Quantity:              in.Quantity.String(),
		FilledQuantity:        "0",
		ReservedQuoteNotional: amount.String(),
		TimeInForce:           string(in.TimeInForce),
		ReduceOnly:            in.ReduceOnly,
		State:                 string(domain.OrderStateIntent),
		CancelState:           string(domain.CancelStateNone),
		OwnerId:               in.Authority.OwnerID,
		OwnerEpoch:            in.Authority.OwnerEpoch,
		ExecutionOwnerId:      execution.OwnerID,
		ExecutionEpoch:        execution.Epoch,
		SubmissionDeadlineAt:  in.Deadline.UnixMilli(),
		Revision:              1,
	}
	if err := tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).Create(row).Error; err != nil {
		return nil, err
	}
	return row, appendAcceptedEvent(ctx, tx, row, now)
}

func validateRiskSnapshot(ctx context.Context, tx *gorm.DB, strategy *modelv1.StrategyInstance) (*modelv1.RiskSnapshot, int64, error) {
	if strategy.State != string(domain.StrategyStateRunning) || !strategy.DesiredRunning || strategy.ActiveMarketDataSource == nil || strategy.ActiveConfigVersion == nil {
		return nil, 0, domain.ErrRiskDenied
	}
	var snapshot modelv1.RiskSnapshot
	if err := tx.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ? AND delete_at IS NULL", strategy.Id).
		Clauses(clause.Locking{Strength: "SHARE"}).First(&snapshot).Error; err != nil {
		return nil, 0, err
	}
	var clock struct{ NowMS int64 }
	if err := tx.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("id = ?", snapshot.Id).
		Select("floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint AS now_ms").Take(&clock).Error; err != nil {
		return nil, 0, err
	}
	if snapshot.AccountId != strategy.AccountId || snapshot.InstrumentId != strategy.InstrumentId || snapshot.ConfigVersion != *strategy.ActiveConfigVersion ||
		snapshot.MarketDataSource != *strategy.ActiveMarketDataSource || !snapshot.Reconciled || !snapshot.AllowNewRisk ||
		snapshot.ObservedAt > clock.NowMS || snapshot.ExpiresAt <= clock.NowMS || clock.NowMS-snapshot.ObservedAt > domain.MaxRiskSnapshotAge.Milliseconds() {
		return nil, 0, domain.ErrRiskDenied
	}
	now, err := validateRiskMarket(ctx, tx, strategy, &snapshot)
	if err != nil {
		return nil, 0, err
	}
	return &snapshot, now, nil
}

func lockIntentInstrument(ctx context.Context, tx *gorm.DB, instrumentID int64) (*modelv1.Instrument, error) {
	var instrument modelv1.Instrument
	err := tx.WithContext(ctx).Model(&modelv1.Instrument{}).Where("delete_at IS NULL").Clauses(clause.Locking{Strength: "SHARE"}).First(&instrument, instrumentID).Error
	return &instrument, err
}

func reserveWithinLimit(reservedText, limitText string, amount decimal.Decimal) (decimal.Decimal, error) {
	reserved, err := decimal.NewFromString(reservedText)
	if err != nil {
		return decimal.Zero, err
	}
	limit, err := decimal.NewFromString(limitText)
	if err != nil {
		return decimal.Zero, err
	}
	next := reserved.Add(amount)
	if next.IsNegative() || next.GreaterThan(limit) {
		return decimal.Zero, domain.ErrRiskDenied
	}
	return next, nil
}

func saveAccountReservation(ctx context.Context, tx *gorm.DB, row *modelv1.AccountBudget, reserved decimal.Decimal) error {
	previous := row.Revision
	row.Revision++
	row.ReservedNotional = reserved.String()
	result := tx.WithContext(ctx).Model(&modelv1.AccountBudget{}).Where("id = ? AND revision = ?", row.Id, previous).
		Select("ReservedNotional", "Revision").Updates(row)
	return reservationResult(result)
}

func saveStrategyReservation(ctx context.Context, tx *gorm.DB, row *modelv1.StrategyBudget, reserved decimal.Decimal) error {
	previous := row.Revision
	row.Revision++
	row.ReservedNotional = reserved.String()
	result := tx.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Where("id = ? AND revision = ?", row.Id, previous).
		Select("ReservedNotional", "Revision").Updates(row)
	return reservationResult(result)
}

func reservationResult(result *gorm.DB) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return domain.ErrIdempotencyConflict
	}
	return nil
}

func appendAcceptedEvent(ctx context.Context, tx *gorm.DB, row *modelv1.OrderIntent, now int64) error {
	return appendIntentEvent(ctx, tx, row, now, domain.EventOrderIntentAccepted)
}

func appendIntentEvent(ctx context.Context, tx *gorm.DB, row *modelv1.OrderIntent, now int64, kind string) error {
	eventID := fmt.Sprintf("intent:%d:%d", row.Id, row.Revision)
	state, err := intentWireState(domain.OrderState(row.State))
	if err != nil {
		return err
	}
	cancelState, err := intentWireCancelState(domain.CancelState(row.CancelState))
	if err != nil {
		return err
	}
	side := commonv1.OrderSide_ORDER_SIDE_BUY
	if row.Side == string(domain.SideSell) {
		side = commonv1.OrderSide_ORDER_SIDE_SELL
	}
	event := &commonv1.ExecutionEvent{
		EventId:          eventID,
		EventType:        kind,
		SchemaVersion:    domain.EventSchemaVersion,
		OccurredAtMs:     now,
		Producer:         domain.EventProducerExecution,
		AggregateId:      row.Id,
		AggregateVersion: row.Revision,
		RequestId:        row.RequestId,
		Intent: &commonv1.IntentEvent{
			IntentId:              row.Id,
			ClientOrderId:         row.ClientOrderId,
			AccountId:             row.AccountId,
			StrategyInstanceId:    row.StrategyInstanceId,
			ConfigVersion:         row.ConfigVersion,
			InstrumentId:          row.InstrumentId,
			Side:                  side,
			Price:                 row.Price,
			Quantity:              row.Quantity,
			ReservedQuoteNotional: row.ReservedQuoteNotional,
			State:                 state,
			Revision:              row.Revision,
			CoreOrderId:           row.GetCoreOrderId(),
			CoreRevision:          row.CoreRevision,
			FilledQuantity:        row.FilledQuantity,
			CancelState:           cancelState,
			ErrorCode:             row.ErrorCode,
		},
	}
	return tx.WithContext(ctx).Model(&modelv1.OutboxEvent{}).Create(&modelv1.OutboxEvent{
		EventId:       eventID,
		IntentId:      row.Id,
		EventType:     kind,
		Event:         event,
		State:         domain.OutboxStatePending,
		NextAttemptAt: now,
	}).Error
}
