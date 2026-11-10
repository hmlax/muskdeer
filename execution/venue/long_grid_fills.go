package venue

import (
	"context"
	"strconv"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	model "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/longgrid"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

func (r *Runtime) resolveGridAction(ctx context.Context, id *int64, client gateway.LongGridClient, precision int32) error {
	if id == nil {
		return nil
	}
	var action model.LongGridAction
	if err := gridDB(ctx, r.db).Model(&model.LongGridAction{}).Where("id = ? AND instance_id = ?", *id, r.cfg.InstanceID).Take(&action).Error; err != nil {
		return err
	}
	if action.State == longgrid.Rejected {
		return r.applyGridFill(ctx, &action, nil)
	}
	orderID := uint64(0)
	if action.OrderId != "" {
		var err error
		orderID, err = strconv.ParseUint(action.OrderId, 10, 64)
		if err != nil {
			return err
		}
	}
	order, err := client.FindGridOrder(ctx, gridScope(r.cfg.Venue.Profiles[0]), action.ClientId, orderID, precision)
	if err != nil {
		return err
	}
	return r.applyGridFill(ctx, &action, order)
}

func (r *Runtime) applyGridFill(ctx context.Context, action *model.LongGridAction, order *gateway.GridOrder) error {
	return gridDB(ctx, r.db).Model(&model.LongGridRuntime{}).Transaction(func(tx *gorm.DB) error {
		state, err := lockGridCursor(ctx, tx, r.cfg.InstanceID)
		if err != nil {
			return err
		}
		stop := action.Kind == longgrid.StopLoss
		pointer := state.PendingActionId
		if stop {
			pointer = state.StopActionId
		}
		if pointer == nil || *pointer != action.Id {
			return nil
		}
		if order != nil {
			filled, notional, err := validGridFill(action, order)
			if err != nil {
				return err
			}
			if err := gridApplyLots(state, action, filled, notional, r.cfg.LongGrid); err != nil {
				return err
			}
			action.OrderId = strconv.FormatUint(order.OrderID, 10)
			action.ExecutedQuantity, action.ExecutedNotional = filled.String(), notional.String()
			action.State = longgrid.Terminal
			if err := gridDB(ctx, tx).Model(&model.LongGridAction{}).Where("id = ? AND state IN ?", action.Id, []string{longgrid.Dispatched, longgrid.Acknowledged, longgrid.Unknown}).Select("State", "OrderId", "ExecutedQuantity", "ExecutedNotional").Updates(action).Error; err != nil {
				return err
			}
		}
		if stop {
			state.StopActionId = nil
		} else {
			state.PendingActionId = nil
		}
		if err := saveGridCursor(ctx, tx, state); err != nil {
			return err
		}
		// The caller reloads the cursor and fresh venue positions after this commit.
		return nil
	})
}

func validGridFill(action *model.LongGridAction, order *gateway.GridOrder) (decimal.Decimal, decimal.Decimal, error) {
	filled, e1 := native.Value(order.ExecutedQuantity, false, true)
	notional, e2 := native.Value(order.ExecutedNotional, false, true)
	requested, e3 := domain.ParseLedgerDecimal(action.Quantity, false, false)
	original, e4 := native.Value(order.Quantity, false, false)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || order.ClientID != action.ClientId || order.Side != domain.Side(action.Side) || order.ReduceOnly != (action.Side == string(domain.SideSell)) || !requested.Equal(original) || filled.GreaterThan(requested) || filled.IsPositive() != notional.IsPositive() || !native.Terminal(order.Status) {
		return decimal.Zero, decimal.Zero, longgrid.ErrInvalid
	}
	return filled, notional, nil
}

func gridApplyLots(state *model.LongGridRuntime, action *model.LongGridAction, filled, notional decimal.Decimal, p *common.LongGridParameters) error {
	if filled.IsZero() || action.Kind == longgrid.StopLoss {
		return nil
	}
	if state.Lots == nil {
		state.Lots = &common.LongGridLots{}
	}
	if action.Side == string(domain.SideBuy) {
		price, _ := notional.QuoRem(filled, 18)
		if !price.IsPositive() {
			return longgrid.ErrInvalid
		}
		state.Lots.Items = append(state.Lots.Items, &common.LongGridLot{
			Sequence:   action.Sequence,
			EntryPrice: price.String(),
			Quantity:   filled.String(),
		})
		spacing, err := native.Value(p.SpacingFraction, false, false)
		if err != nil {
			return err
		}
		state.Started = true
		state.NextBuyPrice = price.Mul(decimal.NewFromInt(1).Sub(spacing)).String()
		return nil
	}
	found := false
	var lots []*common.LongGridLot
	for _, lot := range state.Lots.Items {
		if lot.Sequence != action.LotSequence {
			lots = append(lots, lot)
			continue
		}
		found = true
		quantity, err := native.Value(lot.Quantity, false, false)
		if err != nil || filled.GreaterThan(quantity) {
			return longgrid.ErrInvalid
		}
		state.NextBuyPrice = lot.EntryPrice
		remaining := quantity.Sub(filled)
		if remaining.IsPositive() {
			lot.Quantity = remaining.String()
			lots = append(lots, lot)
		}
	}
	if !found {
		return longgrid.ErrInvalid
	}
	state.Lots.Items = lots
	return nil
}
