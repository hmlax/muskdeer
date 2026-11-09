package native

import (
	"time"

	config "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

// WashTarget computes a single current publication, without batches, consumed
// counts, notional accumulation, or persistent execution identities.
func WashTarget(policy *common.VenuePolicy, input *pb.WashTradeRequest) (*common.VenueTarget, error) {
	if policy == nil || input == nil || !policy.GetSyntheticTape().GetDevelopmentOnly() || !policy.SyntheticTape.Enabled {
		return nil, ErrInvalid
	}
	switch policy.SyntheticTape.Mode {
	case config.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE:
		if input.GetBook() != nil {
			return nil, ErrInvalid
		}
		trade := input.GetTrade()
		if ValidateTrade(trade, input.Symbol, time.Now()) != nil || trade.GetPublisher().GetAccountId() != TradeKey(input.Symbol) {
			return nil, ErrInvalid
		}
		return SyntheticTradeTarget(policy, trade)
	case config.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_BOOK:
		return washBookTarget(policy, input)
	default:
		return nil, ErrInvalid
	}
}

func washBookTarget(policy *common.VenuePolicy, input *pb.WashTradeRequest) (*common.VenueTarget, error) {
	book := input.GetBook()
	if input.GetTrade() != nil || !Symbol(input.Symbol) || book.GetSymbol() != input.Symbol || book.GetSource() != Source || book.GetSegment() != Segment || book.GetPublisher().GetAccountId() != MarketKey(input.Symbol) || book.GetValidUntilMs() <= time.Now().UnixMilli() || len(book.GetBids()) == 0 || len(book.GetAsks()) == 0 {
		return nil, ErrInvalid
	}
	profile, market := SyntheticProfile(policy, input.Symbol), SyntheticMarket(policy, input.Symbol)
	if profile == nil || market == nil {
		return nil, ErrInvalid
	}
	side := domain.Side(input.Side)
	price, e1 := Value(book.Asks[0].Price, false, false)
	bid, e2 := Value(book.Bids[0].Price, false, false)
	tick, e3 := Value(market.PriceTick, false, false)
	if e1 != nil || e2 != nil || e3 != nil || !bid.LessThan(price) || side != domain.SideBuy && side != domain.SideSell {
		return nil, ErrInvalid
	}
	if side == domain.SideSell {
		price = bid
	}
	rounding := domain.SideSell
	if side == domain.SideSell {
		rounding = domain.SideBuy
	}
	price, err := domain.RoundPrice(price, tick, rounding)
	if err != nil || !price.IsPositive() {
		return nil, ErrInvalid
	}
	quantity, e1 := Value(profile.Quantity, false, false)
	step, e2 := Value(market.QuantityStep, false, false)
	if e1 != nil || e2 != nil || !quantity.Mod(step).IsZero() {
		return nil, ErrInvalid
	}
	return &common.VenueTarget{
		Side:     string(side),
		Price:    price.String(),
		Quantity: profile.Quantity,
	}, nil
}
