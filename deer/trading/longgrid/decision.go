// Package longgrid implements a long-only percentage grid with priority stop loss.
package longgrid

import (
	"errors"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/shopspring/decimal"
)

// Action and cursor states are shared by calculation, persistence and execution.
const (
	Hold           = "HOLD"
	InitialBuy     = "INITIAL_BUY"
	GridBuy        = "GRID_BUY"
	TakeProfit     = "TAKE_PROFIT"
	StopLoss       = "STOP_LOSS"
	Running        = "RUNNING"
	Stopping       = "STOPPING"
	Stopped        = "STOPPED"
	Prepared       = "PREPARED"
	Dispatched     = "DISPATCHED"
	Acknowledged   = "ACK"
	Unknown        = "UNKNOWN"
	Rejected       = "REJECTED"
	Terminal       = "TERMINAL"
	messageInvalid = "long grid observation or execution cursor is invalid"
)

// ErrInvalid fails closed on an inconsistent observation or cursor.
var ErrInvalid = errors.New(messageInvalid)

// Rules contains exact ratios and quote notionals.
type Rules struct {
	Spacing decimal.Decimal
	Stop    decimal.Decimal
	Order   decimal.Decimal
	Maximum decimal.Decimal
}

// Decision never sells an opening leg. StopLoss exits the complete observed long.
type Decision struct {
	Kind        string
	Side        domain.Side
	Price       decimal.Decimal
	Quantity    decimal.Decimal
	PositionID  string
	LotSequence int64
}

// Parse validates the full concrete parameter set.
func Parse(p *common.LongGridParameters) (Rules, error) {
	spacing, e1 := native.Value(p.GetSpacingFraction(), false, false)
	stop, e2 := native.Value(p.GetStopLossFraction(), false, false)
	order, e3 := native.Value(p.GetOrderNotional(), false, false)
	maximum, e4 := native.Value(p.GetMaximumPositionNotional(), false, false)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || spacing.GreaterThanOrEqual(decimal.NewFromInt(1)) || stop.GreaterThanOrEqual(decimal.NewFromInt(1)) || order.GreaterThan(maximum) || !p.GetOpenOnStart() || !p.GetHaltAfterStopLoss() {
		return Rules{}, ErrInvalid
	}
	return Rules{
		Spacing: spacing,
		Stop:    stop,
		Order:   order,
		Maximum: maximum,
	}, nil
}

// Position returns one existing venue long; short or ambiguous hedge rows fail.
func Position(snapshot *common.VenueSnapshot) (decimal.Decimal, decimal.Decimal, string, error) {
	quantity, entry := decimal.Zero, decimal.Zero
	var id string
	for _, position := range snapshot.GetPositions() {
		q, e1 := native.Value(position.SignedQuantity, true, true)
		p, e2 := native.Value(position.EntryPrice, false, true)
		if e1 != nil || e2 != nil || q.IsNegative() || position.Symbol != snapshot.GetScope().GetSymbol() {
			return decimal.Zero, decimal.Zero, "", ErrInvalid
		}
		if q.IsZero() {
			continue
		}
		if !quantity.IsZero() || !p.IsPositive() || position.PositionId == "" {
			return decimal.Zero, decimal.Zero, "", ErrInvalid
		}
		quantity, entry, id = q, p, position.PositionId
	}
	return quantity, entry, id, nil
}

// Decide evaluates stop loss before grid adds or profit reductions. Disabled
// opening never disables a latched stop or an already-held position's stop loss.
func Decide(r Rules, lots *common.LongGridLots, nextBuy decimal.Decimal, started bool, snapshot *common.VenueSnapshot, bidText, askText string, allowOpen, stopping bool) (Decision, error) {
	quantity, entry, id, err := Position(snapshot)
	if err != nil {
		return Decision{}, err
	}
	mark, err := native.Value(snapshot.GetMarket().GetMarkPrice(), false, false)
	if err != nil {
		return Decision{}, err
	}
	stopPrice := mark
	if bid, err := native.Value(bidText, false, false); err == nil {
		stopPrice = bid
	}
	if quantity.IsPositive() && (stopping || stopPrice.LessThanOrEqual(entry.Mul(decimal.NewFromInt(1).Sub(r.Stop)))) {
		return Decision{
			Kind:       StopLoss,
			Side:       domain.SideSell,
			Quantity:   quantity,
			PositionID: id,
		}, nil
	}
	if stopping || !allowOpen {
		return Decision{Kind: Hold}, nil
	}
	total, err := LotQuantity(lots)
	if err != nil || !total.Equal(quantity) {
		return Decision{}, ErrInvalid
	}
	bid, e1 := native.Value(bidText, false, false)
	ask, e2 := native.Value(askText, false, false)
	if e1 != nil || e2 != nil || !bid.LessThan(ask) {
		return Decision{}, ErrInvalid
	}
	if quantity.IsPositive() {
		decision, err := profitDecision(r, lots, snapshot, bid, id)
		if err != nil || decision.Kind != Hold {
			return decision, err
		}
	}
	if !started || nextBuy.IsPositive() && ask.LessThanOrEqual(nextBuy) {
		return buyDecision(r, snapshot, mark, quantity, nextBuy, started)
	}
	return Decision{Kind: Hold}, nil
}

// LotQuantity checks every retained entry lot before further opening.
func LotQuantity(lots *common.LongGridLots) (decimal.Decimal, error) {
	total := decimal.Zero
	for _, lot := range lots.GetItems() {
		q, e1 := native.Value(lot.Quantity, false, false)
		_, e2 := native.Value(lot.EntryPrice, false, false)
		if e1 != nil || e2 != nil || lot.Sequence < 0 {
			return decimal.Zero, ErrInvalid
		}
		total = total.Add(q)
	}
	return total, nil
}

func profitDecision(r Rules, lots *common.LongGridLots, snapshot *common.VenueSnapshot, bid decimal.Decimal, id string) (Decision, error) {
	nativeBid, e1 := native.Value(snapshot.GetMarket().GetBestBid(), false, false)
	tick, e2 := native.Value(snapshot.GetMarket().GetPriceTick(), false, false)
	if e1 != nil || e2 != nil {
		return Decision{}, ErrInvalid
	}
	var selected *common.LongGridLot
	selectedPrice := decimal.Zero
	for _, lot := range lots.GetItems() {
		entry, err := native.Value(lot.EntryPrice, false, false)
		if err != nil {
			return Decision{}, err
		}
		price, err := domain.RoundPrice(entry.Mul(decimal.NewFromInt(1).Add(r.Spacing)), tick, domain.SideSell)
		if err != nil {
			return Decision{}, err
		}
		if bid.GreaterThanOrEqual(price) && nativeBid.GreaterThanOrEqual(price) && (selected == nil || price.LessThan(selectedPrice)) {
			selected, selectedPrice = lot, price
		}
	}
	if selected == nil {
		return Decision{Kind: Hold}, nil
	}
	quantity, err := native.Value(selected.Quantity, false, false)
	return Decision{
		Kind:        TakeProfit,
		Side:        domain.SideSell,
		Price:       selectedPrice,
		Quantity:    quantity,
		PositionID:  id,
		LotSequence: selected.Sequence,
	}, err
}

func buyDecision(r Rules, snapshot *common.VenueSnapshot, mark, position, nextBuy decimal.Decimal, started bool) (Decision, error) {
	market := snapshot.GetMarket()
	price, e1 := native.Value(market.GetBestAsk(), false, false)
	tick, e2 := native.Value(market.GetPriceTick(), false, false)
	step, e3 := native.Value(market.GetQuantityStep(), false, false)
	minimum, e4 := native.Value(market.GetMinimumQuantity(), false, true)
	minNotional, e5 := native.Value(market.GetMinimumNotional(), false, true)
	maximum, e6 := native.Value(market.GetMaximumQuantity(), false, true)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || e6 != nil {
		return Decision{}, ErrInvalid
	}
	price, err := domain.RoundPrice(price, tick, domain.SideSell)
	if err != nil {
		return Decision{}, err
	}
	if started && price.GreaterThan(nextBuy) {
		return Decision{Kind: Hold}, nil
	}
	lots, _ := r.Order.QuoRem(price.Mul(step), 0)
	quantity := lots.Mul(step)
	if maximum.IsPositive() {
		quantity, _ = domain.RoundQuantity(decimal.Min(quantity, maximum), step)
	}
	if !quantity.IsPositive() || quantity.LessThan(minimum) || quantity.Mul(price).LessThan(minNotional) || position.Mul(mark).Add(quantity.Mul(price)).GreaterThan(r.Maximum) {
		return Decision{Kind: Hold}, nil
	}
	kind := InitialBuy
	if started {
		kind = GridBuy
	}
	return Decision{
		Kind:     kind,
		Side:     domain.SideBuy,
		Price:    price,
		Quantity: quantity,
	}, nil
}
