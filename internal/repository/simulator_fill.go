package repository

import (
	"context"
	"errors"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func (s *simulator) Fill(ctx context.Context, request domain.CoreFillRequest) (domain.CoreFill, error) {
	if err := coreContext(ctx); err != nil {
		return domain.CoreFill{}, err
	}
	if !coreIdentifier(request.TradeID, 128) || !coreIdentifier(request.CounterpartySubject, 64) || !domain.FixedDecimal(request.Price, 20, 18, false) || !domain.FixedDecimal(request.Quantity, 20, 18, false) {
		return domain.CoreFill{}, domain.ErrInvalidOrder
	}
	initial, err := s.Find(ctx, request.AccountID, request.ClientOrderID)
	if err != nil {
		return domain.CoreFill{}, err
	}
	var result domain.CoreFill
	err = s.db.WithContext(ctx).Model(&modelv1.SimulatorFill{}).Transaction(func(tx *gorm.DB) error {
		rows, err := lockCoreRows(ctx, tx, request.AccountID, initial.InstrumentID)
		if err != nil {
			return err
		}
		order, err := findCoreOrder(ctx, tx, request.AccountID, request.ClientOrderID)
		if err != nil {
			return err
		}
		var existing modelv1.SimulatorFill
		err = tx.WithContext(ctx).Model(&modelv1.SimulatorFill{}).Where("trade_id = ?", request.TradeID).First(&existing).Error
		if err == nil {
			result, err = coreFill(&existing)
			if err != nil {
				return err
			}
			if result.CoreOrderID != order.Id || !result.Price.Equal(request.Price) || !result.Quantity.Equal(request.Quantity) || existing.CounterpartySubject != request.CounterpartySubject {
				return domain.ErrIdempotencyConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if rows.account.SubjectId == request.CounterpartySubject {
			return domain.ErrSelfTrade
		}
		if order.State != string(domain.OrderStateLive) && order.State != string(domain.OrderStatePartiallyFilled) {
			return domain.ErrInvalidOrder
		}
		if err := validateCoreFillPrecision(request, rows.instrument); err != nil {
			return err
		}
		fact, err := stageCoreFill(ctx, tx, rows, order, request)
		if err != nil {
			return err
		}
		result, err = coreFill(fact)
		return err
	})
	if err != nil {
		return domain.CoreFill{}, coreFailure(ctx, err)
	}
	return result, nil
}

func validateCoreFillPrecision(request domain.CoreFillRequest, instrument *modelv1.Instrument) error {
	tick, err := decimal.NewFromString(instrument.PriceTick)
	if err != nil {
		return err
	}
	step, err := decimal.NewFromString(instrument.QuantityStep)
	if err != nil {
		return err
	}
	if !request.Price.Mod(tick).IsZero() || !request.Quantity.Mod(step).IsZero() {
		return domain.ErrInvalidOrder
	}
	return nil
}

func stageCoreFill(ctx context.Context, tx *gorm.DB, rows coreRows, row *modelv1.SimulatorOrder, request domain.CoreFillRequest) (*modelv1.SimulatorFill, error) {
	order, err := coreOrder(row)
	if err != nil {
		return nil, err
	}
	settlement, err := domain.SettleSpotFill(order, request.Price, request.Quantity)
	if err != nil {
		return nil, err
	}
	base, quote, err := lockBalances(ctx, tx, rows)
	if err != nil {
		return nil, err
	}
	funding, receiving := base, quote
	if order.Side == domain.SideBuy {
		funding, receiving = quote, base
	}
	if err := adjustBalance(ctx, tx, funding, settlement.Released, settlement.Spent.Add(settlement.Released).Neg()); err != nil {
		return nil, err
	}
	if err := adjustBalance(ctx, tx, receiving, settlement.Received, decimal.Zero); err != nil {
		return nil, err
	}
	row.FilledQuantity = order.FilledQuantity.Add(request.Quantity).String()
	row.FrozenAmount = settlement.RemainingFunds.String()
	row.State = string(domain.OrderStatePartiallyFilled)
	if order.FilledQuantity.Add(request.Quantity).Equal(order.Quantity) {
		row.State = string(domain.OrderStateFilled)
		if row.CancelState == string(domain.CancelStatePending) {
			row.CancelState = string(domain.CancelStateRejected)
		}
	}
	if err := saveCoreOrder(ctx, tx, row); err != nil {
		return nil, err
	}
	fill := &modelv1.SimulatorFill{
		TradeId:             request.TradeID,
		CoreOrderId:         row.Id,
		Price:               request.Price.String(),
		Quantity:            request.Quantity.String(),
		QuoteNotional:       settlement.Notional.String(),
		FeeAmount:           settlement.Fee.String(),
		FeeAsset:            rows.instrument.QuoteAsset,
		CounterpartySubject: request.CounterpartySubject,
	}
	if err := tx.WithContext(ctx).Model(&modelv1.SimulatorFill{}).Create(fill).Error; err != nil {
		return nil, err
	}
	if err := appendSimulatorFillEvent(ctx, tx, row, fill); err != nil {
		return nil, err
	}
	if err := invalidateUnprojectedRisk(ctx, tx, rows.account.AccountId); err != nil {
		return nil, err
	}
	return fill, advanceCoreAccount(ctx, tx, rows.account)
}
