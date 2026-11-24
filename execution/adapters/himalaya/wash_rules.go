package himalaya

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
	pb "gitlab.com/himalaya/lukla-go/pb"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

const (
	washMarketRulesAge = time.Second
	washMarketActive   = 1
)

// Rules contain no account state. Only successful, active SDK markets are cached.
type washMarketRules struct {
	tick            decimal.Decimal
	step            decimal.Decimal
	minimumQuantity decimal.Decimal
	maximumQuantity decimal.Decimal
	minimumNotional decimal.Decimal
	validUntil      time.Time
}

func (c *Client) cachedWashRules(symbol string) *washMarketRules {
	value, ok := c.washMarkets.Load(symbol)
	if !ok {
		return nil
	}
	rules := value.(*washMarketRules)
	if !time.Now().Before(rules.validUntil) {
		return nil
	}
	return rules
}

func (c *Client) washRules(ctx context.Context, symbol string) (*washMarketRules, error) {
	if rules := c.cachedWashRules(symbol); rules != nil {
		return rules, nil
	}
	pending := c.washMarketLoads.DoChan(symbol, func() (any, error) {
		if rules := c.cachedWashRules(symbol); rules != nil {
			return rules, nil
		}
		started := time.Now()
		reply, err := c.sdk.Raw().GetMarket(ctx, &pb.GetMarketRequest{Symbol: symbol})
		if err != nil {
			return nil, err
		}
		rules, err := washRulesFromMarket(reply.GetMarket(), symbol, started.Add(washMarketRulesAge))
		if err != nil || !time.Now().Before(started.Add(washMarketRulesAge)) {
			return nil, ErrInvalidSnapshot
		}
		c.washMarkets.Store(symbol, rules)
		return rules, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-pending:
		if result.Err != nil {
			return nil, result.Err
		}
		return result.Val.(*washMarketRules), nil
	}
}

func washRulesFromMarket(market *pb.MarketView, symbol string, until time.Time) (*washMarketRules, error) {
	if market.GetSymbol() != symbol || market.GetStatus() != washMarketActive || market.EffectiveBasePrecision < 0 || market.EffectiveBasePrecision > 18 || market.EffectiveQuotePrecision < 0 || market.EffectiveQuotePrecision > 18 || market.PriceTickTicks <= 0 || market.BaseIncrement <= 0 || market.BaseMinSize < 0 || market.BaseMaxSize < 0 || market.QuoteMinSize < 0 {
		return nil, ErrInvalidSnapshot
	}
	base, quote := -int32(market.EffectiveBasePrecision), -int32(market.EffectiveQuotePrecision)
	rules := &washMarketRules{
		tick:            decimal.NewFromInt(market.PriceTickTicks).Shift(quote),
		step:            decimal.NewFromInt(market.BaseIncrement).Shift(base),
		minimumQuantity: decimal.NewFromInt(market.BaseMinSize).Shift(base),
		maximumQuantity: decimal.NewFromInt(market.BaseMaxSize).Shift(base),
		// Matching intentionally expresses quote_min_size in legacy 10^-8 quote
		// units, independently of effective price precision.
		minimumNotional: decimal.NewFromInt(market.QuoteMinSize).Shift(-8),
		validUntil:      until,
	}
	if rules.maximumQuantity.IsPositive() && rules.maximumQuantity.LessThan(rules.minimumQuantity) {
		return nil, ErrInvalidSnapshot
	}
	return rules, nil
}

func (r *washMarketRules) validate(input *gateway.SyntheticRequest) error {
	price, e1 := native.Value(input.Price, false, false)
	quantity, e2 := native.Value(input.Quantity, false, false)
	if e1 != nil || e2 != nil || !price.Mod(r.tick).IsZero() || !quantity.Mod(r.step).IsZero() {
		return ErrInvalidSubmit
	}
	if quantity.LessThan(r.minimumQuantity) || price.Mul(quantity).LessThan(r.minimumNotional) {
		return gateway.ErrBelowMarketMinimum
	}
	if r.maximumQuantity.IsPositive() && quantity.GreaterThan(r.maximumQuantity) {
		return ErrInvalidSubmit
	}
	return nil
}

// WashMarket returns only current venue size/price rules, without account state.
func (c *Client) WashMarket(ctx context.Context, symbol string) (*gateway.WashMarketRules, error) {
	if c == nil || c.sdk == nil || ctx == nil || !validSymbol(symbol) {
		return nil, ErrInvalidSubmit
	}
	op, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	rules, err := c.washRules(op, symbol)
	if err != nil {
		return nil, err
	}
	return &gateway.WashMarketRules{
		Market: &common.VenueMarket{
			PriceTick:       rules.tick.String(),
			QuantityStep:    rules.step.String(),
			MinimumQuantity: rules.minimumQuantity.String(),
			MaximumQuantity: rules.maximumQuantity.String(),
			MinimumNotional: rules.minimumNotional.String(),
		},
		ValidUntil: rules.validUntil,
	}, nil
}
