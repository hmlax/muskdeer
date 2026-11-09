package himalaya

import (
	"context"

	"github.com/shopspring/decimal"
	pb "gitlab.com/himalaya/lukla-go/pb"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

// PublishSynthetic maps one admitted development event to the supplied SDK.
func (c *Client) PublishSynthetic(ctx context.Context, input *gateway.SyntheticRequest) (*gateway.SyntheticResult, error) {
	if c == nil || c.sdk == nil || c.guard == nil || input == nil {
		return nil, ErrInvalidSubmit
	}
	return c.WashTrade(ctx, input)
}

// WashTrade sends one development publication without any persistence guard.
func (c *Client) WashTrade(ctx context.Context, input *gateway.SyntheticRequest) (*gateway.SyntheticResult, error) {
	if c == nil || c.sdk == nil || ctx == nil || input == nil || !validSymbol(input.Symbol) {
		return nil, ErrInvalidSubmit
	}
	if _, err := native.Value(input.Price, false, false); err != nil {
		return nil, ErrInvalidSubmit
	}
	if _, err := native.Value(input.Quantity, false, false); err != nil {
		return nil, ErrInvalidSubmit
	}
	var side int32
	switch input.Side {
	case domain.SideBuy:
		side = int32(pb.Side_SIDE_BUY)
	case domain.SideSell:
		side = int32(pb.Side_SIDE_SELL)
	default:
		return nil, ErrInvalidSubmit
	}
	op, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	response, err := c.sdk.WashTrade(op, &pb.WashTradeRequest{
		Symbol:   input.Symbol,
		Side:     side,
		Price:    input.Price,
		Quantity: input.Quantity,
	})
	if response == nil {
		return nil, err
	}
	return &gateway.SyntheticResult{
		TradeID:     response.TradeId,
		Price:       response.Price,
		Quantity:    response.Quantity,
		TimestampMS: response.TimestampMs,
	}, err
}

// CheckSyntheticMarket checks SDK precision and product bounds for one event.
func (c *Client) CheckSyntheticMarket(ctx context.Context, profile *commonv1.VenueProfilePolicy, plan *commonv1.SyntheticTapePlan) error {
	if c == nil || c.sdk == nil || profile == nil {
		return ErrInvalidSubmit
	}
	response, err := c.sdk.Raw().GetMarket(ctx, &pb.GetMarketRequest{Symbol: plan.Symbol})
	if err != nil {
		return err
	}
	market := response.GetMarket()
	if market.GetSymbol() != plan.Symbol || !response.GetHealth().GetOk() || market.Status != 1 || market.EffectiveBasePrecision > 18 || market.EffectiveQuotePrecision > 18 {
		return ErrInvalidSnapshot
	}
	base, quote := -int32(market.EffectiveBasePrecision), -int32(market.EffectiveQuotePrecision)
	tick := decimal.NewFromInt(market.PriceTickTicks).Shift(quote)
	step := decimal.NewFromInt(market.BaseIncrement).Shift(base)
	if tick.String() != profile.PriceTick || step.String() != profile.QuantityStep {
		return ErrInvalidSnapshot
	}
	quantity, e1 := native.Value(plan.Quantity, false, false)
	price, e2 := native.Value(plan.Price, false, false)
	if e1 != nil || e2 != nil || quantity.LessThan(decimal.NewFromInt(market.BaseMinSize).Shift(base)) ||
		market.BaseMaxSize > 0 && quantity.GreaterThan(decimal.NewFromInt(market.BaseMaxSize).Shift(base)) ||
		price.Mul(quantity).LessThan(decimal.NewFromInt(market.QuoteMinSize).Shift(-8)) {
		return ErrInvalidSubmit
	}
	return nil
}
