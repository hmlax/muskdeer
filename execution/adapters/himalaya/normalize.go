package himalaya

import (
	"strconv"

	"github.com/shopspring/decimal"
	pb "gitlab.com/himalaya/lukla-go/pb"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

// Normalize translates raw SDK integer ticks at the boundary and rejects
// non-ONE_WAY/non-normal funds and positions outside the configured instrument.
func Normalize(input *SDKSnapshot, scope Scope) (*commonv1.VenueSnapshot, error) {
	return normalizeSDK(input, scope, false)
}

func normalizeSDK(input *SDKSnapshot, scope Scope, grid bool) (*commonv1.VenueSnapshot, error) {
	if err := validateNativeSnapshotMode(input, scope, grid); err != nil {
		return nil, err
	}
	market := input.Market.Market
	maker, _ := native.Value(market.MakerFeeRate, true, true)
	taker, _ := native.Value(market.TakerFeeRate, true, true)
	base, quote := -int32(market.EffectiveBasePrecision), -int32(market.EffectiveQuotePrecision)
	result := &commonv1.VenueSnapshot{
		Scope: &commonv1.VenueScope{
			UserId: scope.UserID,
			FundId: scope.FundID,
			Symbol: scope.Symbol,
		},
		Market: &commonv1.VenueMarket{
			PriceTick:       decimal.NewFromInt(market.PriceTickTicks).Shift(quote).String(),
			QuantityStep:    decimal.NewFromInt(market.BaseIncrement).Shift(base).String(),
			MinimumQuantity: decimal.NewFromInt(market.BaseMinSize).Shift(base).String(),
			MaximumQuantity: decimal.NewFromInt(market.BaseMaxSize).Shift(base).String(),
			MinimumNotional: decimal.NewFromInt(market.QuoteMinSize).Shift(-8).String(),
			MarkPrice:       decimal.NewFromUint64(input.Market.Health.MarkPriceTicks).Shift(quote).String(),
			MaximumLeverage: uint32(market.MaxLeverage),
			WorstFeeRate:    decimal.Max(decimal.Zero, maker, taker).String(),
			BestBid:         decimal.NewFromUint64(input.Market.Health.BestBidTicks).Shift(quote).String(),
			BestAsk:         decimal.NewFromUint64(input.Market.Health.BestAskTicks).Shift(quote).String(),
		},
		Available:    input.Account.Available,
		StartedAtMs:  input.StartedAt.UnixMilli(),
		ObservedAtMs: input.ReadAt.UnixMilli(),
		ValidUntilMs: input.StartedAt.Add(native.SnapshotAge).UnixMilli(),
	}
	for _, row := range input.Positions {
		position, err := normalizePosition(row, scope)
		if err != nil {
			return nil, err
		}
		result.Positions = append(result.Positions, position)
	}
	for _, row := range input.Orders {
		order, err := normalizeOrder(row, scope, base)
		if grid {
			order, err = normalizeGridOrder(row, scope, base, result.Market.MarkPrice)
		}
		if err != nil {
			return nil, err
		}
		result.Orders = append(result.Orders, order)
	}
	return result, nil
}

func normalizeOrder(row *pb.OrderRow, scope Scope, base int32) (*commonv1.VenueOrder, error) {
	if row == nil || row.OrderId == 0 || row.UserId != scope.UserID || row.FundId != scope.FundID || row.Symbol != scope.Symbol ||
		row.ExecutedQuantity > row.Quantity || row.Quantity == 0 || !native.Active(row.Status) && !native.Terminal(row.Status) {
		return nil, ErrInvalidSnapshot
	}
	if _, err := native.Value(row.Price, false, false); err != nil {
		return nil, ErrInvalidSnapshot
	}
	var side domain.Side
	switch row.Side {
	case pb.Side_SIDE_BUY:
		side = domain.SideBuy
	case pb.Side_SIDE_SELL:
		side = domain.SideSell
	default:
		return nil, ErrInvalidSnapshot
	}
	return &commonv1.VenueOrder{
		OrderId:          strconv.FormatUint(row.OrderId, 10),
		ClientId:         row.ClientId,
		Side:             string(side),
		Price:            row.Price,
		Quantity:         decimal.NewFromUint64(row.Quantity).Shift(base).String(),
		ExecutedQuantity: decimal.NewFromUint64(row.ExecutedQuantity).Shift(base).String(),
		Status:           row.Status,
	}, nil
}

func normalizePosition(row *pb.PositionRow, scope Scope) (*commonv1.VenuePosition, error) {
	if row == nil || row.UserId != scope.UserID || row.FundId != scope.FundID || row.Symbol != scope.Symbol || row.PositionType != pb.PositionType_POSITION_TYPE_CROSS {
		return nil, ErrInvalidSnapshot
	}
	quantity, err := native.Value(row.Size, false, true)
	if err != nil {
		return nil, ErrInvalidSnapshot
	}
	switch row.PositionDirection {
	case pb.PositionDirection_POSITION_DIRECTION_SHORT:
		quantity = quantity.Neg()
	case pb.PositionDirection_POSITION_DIRECTION_LONG:
	default:
		return nil, ErrInvalidSnapshot
	}
	if _, err := native.Value(row.EntryPrice, false, true); err != nil {
		return nil, ErrInvalidSnapshot
	}
	if _, err := native.Value(row.Leverage, false, false); err != nil {
		return nil, ErrInvalidSnapshot
	}
	return &commonv1.VenuePosition{
		Symbol:         row.Symbol,
		SignedQuantity: quantity.String(),
		PositionId:     row.PositionId,
		EntryPrice:     row.EntryPrice,
		Leverage:       row.Leverage,
	}, nil
}

func validateNativeSnapshot(input *SDKSnapshot, scope Scope) error {
	return validateNativeSnapshotMode(input, scope, false)
}

func validateNativeSnapshotMode(input *SDKSnapshot, scope Scope, grid bool) error {
	if scope.UserID <= 0 || scope.FundID <= 0 || !native.Symbol(scope.Symbol) || input == nil || input.Account == nil || input.Market == nil || input.Market.Market == nil ||
		input.Account.UserId != scope.UserID || input.Account.FundId != scope.FundID ||
		!validGridAccountMode(input.Account.PositionMode, grid) || input.Account.Status != pb.AccountStatus_ACCOUNT_STATUS_NORMAL ||
		!input.Market.GetHealth().GetOk() || input.Market.Market.Symbol != scope.Symbol {
		return ErrInvalidSnapshot
	}
	market := input.Market.Market
	if !validNativeMarket(market) || input.Market.Health.MarkPriceTicks == 0 ||
		input.ReadAt.Before(input.StartedAt) || input.ReadAt.Sub(input.StartedAt) >= native.SnapshotAge {
		return ErrInvalidSnapshot
	}

	if _, err := native.Value(input.Account.Available, false, true); err != nil {
		return ErrInvalidSnapshot
	}
	for _, rate := range []string{market.MakerFeeRate, market.TakerFeeRate} {
		if _, err := native.Value(rate, true, true); err != nil {
			return ErrInvalidSnapshot
		}
	}
	return nil
}
func validNativeMarket(market *pb.MarketView) bool {
	return market.EffectiveBasePrecision >= 0 && market.EffectiveBasePrecision <= 18 &&
		market.EffectiveQuotePrecision >= 0 && market.EffectiveQuotePrecision <= 18 &&
		market.PriceTickTicks > 0 && market.BaseIncrement > 0 && market.Status == 1 &&
		market.MaxLeverage > 0 && market.MaxLeverage <= 150 &&
		market.BaseMinSize >= 0 && market.BaseMaxSize >= 0 && market.QuoteMinSize >= 0
}

func validGridAccountMode(mode pb.AccountPositionMode, grid bool) bool {
	return mode == pb.AccountPositionMode_ACCOUNT_POSITION_MODE_ONE_WAY || grid && mode == pb.AccountPositionMode_ACCOUNT_POSITION_MODE_HEDGE_DUAL_SINGLE
}
