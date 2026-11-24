package native

import (
	"errors"

	"github.com/shopspring/decimal"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
)

// MessageWashMinimumAboveLimit identifies incompatible event quantity bounds.
const MessageWashMinimumAboveLimit = "venue minimum quantity exceeds the configured single-event or venue maximum"

// ErrWashMinimumAboveLimit rejects incompatible lower and upper quantity limits.
var ErrWashMinimumAboveLimit = errors.New(MessageWashMinimumAboveLimit)

// WashTargetForMarket raises a current development target to the smallest whole
// lot satisfying both venue minima. It never changes the source or event cap.
func WashTargetForMarket(policy *common.VenuePolicy, input *execution.WashTradeRequest, market *common.VenueMarket) (*common.VenueTarget, error) {
	target, err := WashTarget(policy, input)
	if err != nil {
		return nil, err
	}
	profile := SyntheticMarket(policy, input.Symbol)
	tick, e1 := Value(market.GetPriceTick(), false, false)
	step, e2 := Value(market.GetQuantityStep(), false, false)
	minimum, e3 := Value(market.GetMinimumQuantity(), false, true)
	notional, e4 := Value(market.GetMinimumNotional(), false, true)
	maximum, e5 := Value(market.GetMaximumQuantity(), false, true)
	configuredTick, e6 := Value(profile.GetPriceTick(), false, false)
	configuredStep, e7 := Value(profile.GetQuantityStep(), false, false)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || e6 != nil || e7 != nil || !tick.Equal(configuredTick) || !step.Equal(configuredStep) {
		return nil, ErrInvalid
	}
	price, _ := Value(target.Price, false, false)
	quantity, _ := Value(target.Quantity, false, false)
	capValue, _ := Value(SyntheticProfile(policy, input.Symbol).Quantity, false, false)
	capLots, _ := capValue.QuoRem(step, 0)
	capValue = capLots.Mul(step)
	if maximum.IsPositive() {
		capValue = decimal.Min(capValue, maximum)
	}
	minimumLots := ceilWashLots(minimum, step)
	notionalLots := ceilWashLots(notional, price.Mul(step))
	quantity = decimal.Max(quantity, decimal.Max(minimumLots, notionalLots).Mul(step))
	if quantity.GreaterThan(capValue) {
		return nil, ErrWashMinimumAboveLimit
	}
	target.Quantity = quantity.String()
	return target, nil
}

func ceilWashLots(value, unit decimal.Decimal) decimal.Decimal {
	lots, remainder := value.QuoRem(unit, 0)
	if !remainder.IsZero() {
		lots = lots.Add(decimal.NewFromInt(1))
	}
	return lots
}
