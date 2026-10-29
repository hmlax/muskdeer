package strategywire

import (
	"github.com/shopspring/decimal"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// OrderProto preserves independent OMS and core observations without secrets.
func OrderProto(value domain.AdminOrder) (*strategyv1.StrategyOrder, error) {
	if !orderDecimals(value) {
		return nil, domain.ErrAdminUnavailable
	}
	reply := &strategyv1.StrategyOrder{
		Id:                    value.ID,
		ClientOrderId:         value.ClientOrderID,
		StrategyId:            value.StrategyID,
		AccountId:             value.AccountID,
		InstrumentId:          value.InstrumentID,
		ConfigVersion:         value.ConfigVersion,
		MarketDataSource:      string(value.Source),
		Side:                  string(value.Side),
		Price:                 value.Price.String(),
		Quantity:              value.Quantity.String(),
		FilledQuantity:        value.FilledQuantity.String(),
		ReservedNotional:      value.ReservedNotional.String(),
		TimeInForce:           string(value.TimeInForce),
		ReduceOnly:            value.ReduceOnly,
		State:                 string(value.State),
		CancelState:           string(value.CancelState),
		ConfirmedCoreId:       value.ConfirmedCoreID,
		ConfirmedCoreRevision: value.ConfirmedCoreRevision,
		Revision:              value.Revision,
		ErrorCode:             value.ErrorCode,
		DeadlineMs:            value.Deadline.UnixMilli(),
		CreatedAtMs:           value.CreatedAt.UnixMilli(),
		UpdatedAtMs:           value.UpdatedAt.UnixMilli(),
	}
	if core := value.Core; core != nil {
		if core.ClientOrderID != value.ClientOrderID || core.AccountID != value.AccountID || core.StrategyID != value.StrategyID ||
			core.InstrumentID != value.InstrumentID || core.ConfigVersion != value.ConfigVersion || core.Side != value.Side ||
			!core.Price.Equal(value.Price) || !core.Quantity.Equal(value.Quantity) || core.TimeInForce != value.TimeInForce || core.ReduceOnly != value.ReduceOnly {
			return nil, domain.ErrAdminUnavailable
		}
		reply.Core = &strategyv1.StrategyOrderCore{
			Id:             core.ID,
			State:          string(core.State),
			CancelState:    string(core.CancelState),
			FilledQuantity: core.FilledQuantity.String(),
			FrozenAsset:    core.FrozenAsset,
			FrozenAmount:   core.FrozenAmount.String(),
			QuoteFeeRate:   core.QuoteFeeRate.String(),
			Revision:       core.Revision,
			UpdatedAtMs:    value.CoreUpdatedAt.UnixMilli(),
		}
	}
	if !ValidOrder(reply, value.StrategyID) {
		return nil, domain.ErrAdminUnavailable
	}
	return reply, nil
}

// ValidOrderPage binds the complete response to the original bounded filter.
func ValidOrderPage(value *strategyv1.ListStrategyOrdersReply, filter domain.AdminOrderFilter) bool {
	if value == nil || len(value.ProtoReflect().GetUnknown()) != 0 || filter.Validate() != nil || len(value.Orders) > filter.Page.Limit || value.NextAfterId < 0 {
		return false
	}
	previous := filter.Page.AfterID
	for _, order := range value.Orders {
		if !ValidOrder(order, filter.StrategyID) || order.Id <= previous ||
			filter.State != domain.OrderStateUnspecified && order.State != string(filter.State) ||
			filter.OpenOnly && !openOrder(domain.OrderState(order.State)) {
			return false
		}
		previous = order.Id
	}
	return value.NextAfterId == 0 || len(value.Orders) == filter.Page.Limit && value.NextAfterId == previous
}

// ValidOrder accepts newer core facts without changing the OMS observation.
func ValidOrder(value *strategyv1.StrategyOrder, strategyID int64) bool {
	if !validOrderIdentity(value, strategyID) || !validOrderTerms(value) {
		return false
	}
	quantity, quantityErr := domain.ParseDecimal(value.Quantity, orderParameterRule(false))
	filled, filledErr := domain.ParseDecimal(value.FilledQuantity, orderParameterRule(true))
	_, priceErr := domain.ParseDecimal(value.Price, orderParameterRule(false))
	_, reservedErr := domain.ParseLedgerDecimal(value.ReservedNotional, false, true)
	if quantityErr != nil || filledErr != nil || priceErr != nil || reservedErr != nil || filled.GreaterThan(quantity) {
		return false
	}
	if value.Core == nil {
		return value.ConfirmedCoreId == 0 && value.ConfirmedCoreRevision == 0
	}
	return validOrderCore(value, quantity)
}

func validOrderIdentity(value *strategyv1.StrategyOrder, strategyID int64) bool {
	return value != nil && len(value.ProtoReflect().GetUnknown()) == 0 && strategyID > 0 && value.StrategyId == strategyID &&
		value.Id > 0 && value.InstrumentId > 0 && value.ConfigVersion > 0 && value.Revision > 0 &&
		riskText(value.ClientOrderId, 128) && riskText(value.AccountId, 64) &&
		domain.MarketSource(value.MarketDataSource).ValidateSelection() == nil &&
		value.DeadlineMs > 0 && value.CreatedAtMs > 0 && value.UpdatedAtMs >= value.CreatedAtMs &&
		value.ConfirmedCoreId >= 0 && value.ConfirmedCoreRevision >= 0 &&
		(value.ConfirmedCoreId == 0) == (value.ConfirmedCoreRevision == 0)
}

func validOrderTerms(value *strategyv1.StrategyOrder) bool {
	return (value.Side == string(domain.SideBuy) || value.Side == string(domain.SideSell)) &&
		(value.TimeInForce == string(domain.TimeInForceGTC) || value.TimeInForce == string(domain.TimeInForceIOC) || value.TimeInForce == string(domain.TimeInForcePostOnly)) &&
		domain.ValidOrderState(domain.OrderState(value.State)) && validCancelState(domain.CancelState(value.CancelState)) && validOrderError(value.ErrorCode)
}

func validOrderCore(value *strategyv1.StrategyOrder, quantity decimal.Decimal) bool {
	core := value.Core
	if len(core.ProtoReflect().GetUnknown()) != 0 || core.Id <= 0 || core.Revision <= 0 || core.UpdatedAtMs <= 0 ||
		!riskText(core.FrozenAsset, 32) || !validCancelState(domain.CancelState(core.CancelState)) ||
		!coreOrderState(domain.OrderState(core.State)) ||
		value.ConfirmedCoreId != 0 && value.ConfirmedCoreId != core.Id || value.ConfirmedCoreRevision > core.Revision {
		return false
	}
	filled, filledErr := domain.ParseDecimal(core.FilledQuantity, orderParameterRule(true))
	_, frozenErr := domain.ParseLedgerDecimal(core.FrozenAmount, false, true)
	rate, rateErr := domain.ParseDecimal(core.QuoteFeeRate, domain.DecimalRule{
		IntegerDigits: 1,
		Scale:         18,
		AllowZero:     true,
	})
	return filledErr == nil && frozenErr == nil && rateErr == nil &&
		!filled.GreaterThan(quantity) && rate.LessThan(decimal.NewFromInt(1))
}

func orderDecimals(value domain.AdminOrder) bool {
	if !domain.FixedDecimal(value.Price, 20, 18, false) || !domain.FixedDecimal(value.Quantity, 20, 18, false) ||
		!domain.FixedDecimal(value.FilledQuantity, 20, 18, true) || !domain.FixedDecimal(value.ReservedNotional, 40, 36, true) {
		return false
	}
	core := value.Core
	return core == nil || domain.FixedDecimal(core.FilledQuantity, 20, 18, true) && domain.FixedDecimal(core.FrozenAmount, 40, 36, true) && domain.FixedDecimal(core.QuoteFeeRate, 1, 18, true)
}

func orderParameterRule(allowZero bool) domain.DecimalRule {
	return domain.DecimalRule{
		IntegerDigits: 20,
		Scale:         18,
		AllowZero:     allowZero,
	}
}

func validOrderError(value string) bool {
	switch value {
	case "", domain.IntentErrorOutcomeUnknown, domain.IntentErrorCoreNotFound, domain.IntentErrorCoreRejected, domain.IntentErrorCoreNotAccepted, domain.IntentErrorRiskDenied:
		return true
	default:
		return false
	}
}

func validCancelState(value domain.CancelState) bool {
	switch value {
	case domain.CancelStateNone, domain.CancelStateRequested, domain.CancelStatePending, domain.CancelStateConfirmed, domain.CancelStateUnknown, domain.CancelStateRejected:
		return true
	default:
		return false
	}
}

func openOrder(value domain.OrderState) bool {
	switch value {
	case domain.OrderStateIntent, domain.OrderStateSubmitting, domain.OrderStateLive, domain.OrderStatePartiallyFilled, domain.OrderStateUnknown:
		return true
	default:
		return false
	}
}

func coreOrderState(value domain.OrderState) bool {
	return value == domain.OrderStateLive || value == domain.OrderStatePartiallyFilled || value == domain.OrderStateFilled || value == domain.OrderStateCanceled
}
