package executiongrpc

import (
	"context"

	executionv1 "github.com/hmlax/muskdeer/api/execution/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/orderwire"
)

func (c *strategyClient) Orders(ctx context.Context, filter domain.ExecutionFilter) ([]domain.IntentSnapshot, int64, error) {
	if err := strategyQueryContext(ctx, filter.AccountID); err != nil {
		return nil, 0, err
	}
	if filter.StrategyID < 0 || filter.InstrumentID < 0 || filter.AfterID < 0 || filter.Limit < 1 || filter.Limit > domain.MaximumExecutionPage {
		return nil, 0, domain.ErrInvalidOrder
	}
	reply, err := c.reader.ListOrders(ctx, &executionv1.ListOrdersRequest{
		AccountId:          filter.AccountID,
		StrategyInstanceId: filter.StrategyID,
		InstrumentId:       filter.InstrumentID,
		AfterId:            filter.AfterID,
		PageSize:           int32(filter.Limit),
		OpenOnly:           filter.OpenOnly,
	})
	if err != nil {
		return nil, 0, err
	}
	return strategyOrderPage(reply, filter)
}

func strategyOrderPage(reply *executionv1.ListOrdersReply, filter domain.ExecutionFilter) ([]domain.IntentSnapshot, int64, error) {
	if !validRequest(reply) || len(reply.Orders) > filter.Limit || reply.NextAfterId < 0 {
		return nil, 0, domain.ErrInvalidOrder
	}
	orders := make([]domain.IntentSnapshot, 0, len(reply.Orders))
	last := filter.AfterID
	for _, view := range reply.Orders {
		order, err := orderwire.Snapshot(view)
		if err != nil {
			return nil, 0, err
		}
		if order.ID <= last || order.Input.AccountID != filter.AccountID ||
			filter.StrategyID > 0 && order.Input.Authority.StrategyID != filter.StrategyID ||
			filter.InstrumentID > 0 && order.Input.InstrumentID != filter.InstrumentID || filter.OpenOnly && terminalStrategyObservation(order.State) {
			return nil, 0, domain.ErrInvalidOrder
		}
		orders = append(orders, order)
		last = order.ID
	}
	if reply.NextAfterId > 0 && (len(orders) != filter.Limit || reply.NextAfterId != last) {
		return nil, 0, domain.ErrInvalidOrder
	}
	return orders, reply.NextAfterId, nil
}

func (c *strategyClient) Account(ctx context.Context, accountID string) (domain.ExecutionAccount, error) {
	if err := strategyQueryContext(ctx, accountID); err != nil {
		return domain.ExecutionAccount{}, err
	}
	reply, err := c.reader.GetAccount(ctx, &executionv1.GetAccountRequest{AccountId: accountID})
	if err != nil {
		return domain.ExecutionAccount{}, err
	}
	return strategyAccountObservation(reply, accountID)
}

func strategyAccountObservation(reply *executionv1.AccountReply, accountID string) (domain.ExecutionAccount, error) {
	if !validRequest(reply) || reply.AccountId != accountID || reply.Sequence <= 0 || reply.ObservedAt.CheckValid() != nil ||
		len(reply.ObservedAt.ProtoReflect().GetUnknown()) != 0 || reply.ObservedAt.AsTime().UnixMilli() <= 0 ||
		len(reply.Balances) > domain.MaximumExecutionAssets {
		return domain.ExecutionAccount{}, domain.ErrInvalidOrder
	}
	result := domain.ExecutionAccount{
		AccountID:  accountID,
		Sequence:   reply.Sequence,
		ObservedAt: reply.ObservedAt.AsTime().UTC(),
		Balances:   make([]domain.CoreBalance, 0, len(reply.Balances)),
	}
	seen := make(map[string]struct{}, len(reply.Balances))
	for _, view := range reply.Balances {
		if view == nil || len(view.ProtoReflect().GetUnknown()) != 0 || !boundedIdentifier(view.Asset, 32) {
			return domain.ExecutionAccount{}, domain.ErrInvalidOrder
		}
		if _, duplicate := seen[view.Asset]; duplicate {
			return domain.ExecutionAccount{}, domain.ErrInvalidOrder
		}
		seen[view.Asset] = struct{}{}
		available, err := domain.ParseLedgerDecimal(view.Available, false, true)
		if err != nil {
			return domain.ExecutionAccount{}, domain.ErrInvalidOrder
		}
		frozen, err := domain.ParseLedgerDecimal(view.Frozen, false, true)
		if err != nil {
			return domain.ExecutionAccount{}, domain.ErrInvalidOrder
		}
		result.Balances = append(result.Balances, domain.CoreBalance{
			Asset:     view.Asset,
			Available: available,
			Frozen:    frozen,
		})
	}
	return result, nil
}

func terminalStrategyObservation(state domain.OrderState) bool {
	return state == domain.OrderStateCanceled || state == domain.OrderStateFilled || state == domain.OrderStateRejected
}
