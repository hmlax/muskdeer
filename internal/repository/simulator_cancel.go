package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func (s *simulator) Cancel(ctx context.Context, command domain.GatewayCancel) (domain.CoreOrder, error) {
	in := command.Input
	if err := validateCoreCommand(ctx, in.Authority, command.ExecutionAuthority, in.AccountID, in.ClientOrderID, in.IdempotencyKey, in.InstrumentID, in.Deadline); err != nil {
		return domain.CoreOrder{}, err
	}
	// Identifiers have bounded safe syntax; include the target and config in
	// the immutable fingerprint, independently of the new owner's incarnation.
	digest := sha256.Sum256([]byte(in.AccountID + "\n" + in.ClientOrderID))
	fingerprint := hex.EncodeToString(digest[:])
	var result domain.CoreOrder
	err := s.db.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Transaction(func(tx *gorm.DB) error {
		if _, err := lockGateway(ctx, tx, in.Authority, command.ExecutionAuthority, in.AccountID, in.InstrumentID); err != nil {
			return err
		}
		order, err := findCoreOrder(ctx, tx, in.AccountID, in.ClientOrderID)
		if err != nil {
			return err
		}
		if order.StrategyInstanceId != in.Authority.StrategyID || order.InstrumentId != in.InstrumentID {
			return domain.ErrInvalidOrder
		}
		existing, err := existingCommand(ctx, tx, in.AccountID, in.IdempotencyKey)
		if err == nil {
			if existing.Action != domain.GatewayActionCancel || existing.ClientOrderId != in.ClientOrderID || existing.Fingerprint != fingerprint || existing.ConfigVersion != in.Authority.ConfigVersion {
				return domain.ErrIdempotencyConflict
			}
			result, err = coreOrder(order)
			return err
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		record := gatewayCommand(in.Authority, command.ExecutionAuthority, in.AccountID, in.ClientOrderID, in.IdempotencyKey, in.InstrumentID, in.Deadline.UnixMilli(), domain.GatewayActionCancel, fingerprint)
		if err := tx.WithContext(ctx).Model(&modelv1.SimulatorCommand{}).Create(record).Error; err != nil {
			return err
		}
		order.LastCommandId = record.Id
		order.CancelState = string(domain.CancelStatePending)
		if order.State == string(domain.OrderStateCanceled) || order.State == string(domain.OrderStateFilled) || order.State == string(domain.OrderStateRejected) {
			order.CancelState = string(domain.CancelStateConfirmed)
		}
		if err := saveCoreOrder(ctx, tx, order); err != nil {
			return err
		}
		result, err = coreOrder(order)
		return err
	})
	if err != nil {
		return domain.CoreOrder{}, coreFailure(ctx, err)
	}
	return result, nil
}

func (s *simulator) ConfirmCancel(ctx context.Context, accountID, clientOrderID string) (domain.CoreOrder, error) {
	initial, err := s.Find(ctx, accountID, clientOrderID)
	if err != nil {
		return domain.CoreOrder{}, err
	}
	var result domain.CoreOrder
	err = s.db.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Transaction(func(tx *gorm.DB) error {
		rows, err := lockCoreRows(ctx, tx, accountID, initial.InstrumentID)
		if err != nil {
			return err
		}
		order, err := findCoreOrder(ctx, tx, accountID, clientOrderID)
		if err != nil {
			return err
		}
		if order.State == string(domain.OrderStateCanceled) || order.State == string(domain.OrderStateFilled) || order.State == string(domain.OrderStateRejected) {
			result, err = coreOrder(order)
			return err
		}
		if order.CancelState != string(domain.CancelStatePending) {
			return domain.ErrInvalidOrder
		}
		if err := releaseCoreOrder(ctx, tx, rows, order); err != nil {
			return err
		}
		result, err = coreOrder(order)
		return err
	})
	if err != nil {
		return domain.CoreOrder{}, coreFailure(ctx, err)
	}
	return result, nil
}
