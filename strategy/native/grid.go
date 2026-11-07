package native

import (
	"strings"
	"time"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	values "github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/strategyconfig"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
	"github.com/shopspring/decimal"
)

func instanceTargets(cfg *venueconfig.RuntimeConfig, profile *commonv1.VenueProfilePolicy, book *commonv1.VenueBook, snapshot *commonv1.VenueSnapshot) ([]*commonv1.VenueTarget, error) {
	if cfg.Grid == nil {
		return BuildTargets(cfg.Venue, profile, book, snapshot)
	}
	p, err := strategyconfig.Grid(cfg.Grid)
	if err != nil {
		return nil, err
	}
	if book.GetSource() != profile.MarketDataSource || book.GetSymbol() != profile.Symbol || book.GetValidUntilMs() <= time.Now().UnixMilli() || time.Since(time.UnixMilli(book.GetEventAtMs())) > p.MaxMarketAge || len(book.Bids) == 0 || len(book.Asks) == 0 || snapshot.GetScope().GetSymbol() != profile.Symbol || snapshot.Scope.UserId != profile.UserId || snapshot.Scope.FundId != profile.FundId || snapshot.GetValidUntilMs() <= time.Now().UnixMilli() {
		return nil, values.ErrInvalid
	}
	tick, e1 := values.Value(profile.PriceTick, false, false)
	step, e2 := values.Value(profile.QuantityStep, false, false)
	bid, e3 := values.Value(book.Bids[0].Price, false, false)
	ask, e4 := values.Value(book.Asks[0].Price, false, false)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		return nil, values.ErrInvalid
	}
	buyRoom, sellRoom, localBid, localAsk, err := gridInventory(profile, snapshot, p.InventoryLimit)
	if err != nil {
		return nil, err
	}
	mid, _ := bid.Add(ask).QuoRem(decimal.NewFromInt(2), 19)
	prices, err := domain.GridPrices(p, tick, mid, localBid, localAsk)
	if err != nil {
		return nil, err
	}
	remaining := p.TotalBudget
	var result []*commonv1.VenueTarget
	for _, level := range prices {
		room := buyRoom
		if level.Side == domain.SideSell {
			room = sellRoom
		}
		orderLimit := decimal.Min(p.PerOrderNotional, remaining)
		lots, _ := orderLimit.QuoRem(level.Price.Mul(step), 0)
		candidate := &target{
			side:     level.Side,
			price:    level.Price,
			quantity: lots.Mul(step),
		}
		quantity, err := constrainedQuantity(candidate, snapshot.Market, orderLimit.String(), step)
		if err != nil {
			continue
		}
		quantity, _ = domain.RoundQuantity(decimal.Min(quantity, decimal.Max(decimal.Zero, room)), step)
		minimum, _ := values.Value(snapshot.Market.MinimumQuantity, false, true)
		notional, _ := values.Value(snapshot.Market.MinimumNotional, false, true)
		if !quantity.IsPositive() || quantity.LessThan(minimum) || level.Price.Mul(quantity).LessThan(notional) {
			continue
		}
		remaining = remaining.Sub(level.Price.Mul(quantity))
		if level.Side == domain.SideBuy {
			buyRoom = buyRoom.Sub(quantity)
		} else {
			sellRoom = sellRoom.Sub(quantity)
		}
		result = append(result, &commonv1.VenueTarget{
			Side:     string(level.Side),
			Price:    level.Price.String(),
			Quantity: quantity.String(),
		})
	}
	if len(result) == 0 {
		return nil, values.ErrInvalid
	}
	return result, nil
}

func gridInventory(profile *commonv1.VenueProfilePolicy, snapshot *commonv1.VenueSnapshot, limit decimal.Decimal) (decimal.Decimal, decimal.Decimal, decimal.Decimal, decimal.Decimal, error) {
	net := decimal.Zero
	for _, position := range snapshot.Positions {
		if position.Symbol != profile.Symbol {
			return decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero, values.ErrInvalid
		}
		q, err := values.Value(position.SignedQuantity, true, true)
		if err != nil {
			return decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero, err
		}
		net = net.Add(q)
	}
	localBid, localAsk := decimal.Zero, decimal.Zero
	buyRoom, sellRoom := limit.Sub(net), limit.Add(net)
	for _, order := range snapshot.Orders {
		if !values.Active(order.Status) || strings.HasPrefix(order.ClientId, values.InstanceClientPrefix(profile)) {
			continue
		}
		price, e1 := values.Value(order.Price, false, false)
		quantity, e2 := values.Value(order.Quantity, false, false)
		executed, e3 := values.Value(order.ExecutedQuantity, false, true)
		if e1 != nil || e2 != nil || e3 != nil || executed.GreaterThan(quantity) {
			return decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero, values.ErrInvalid
		}
		if order.Side == string(domain.SideBuy) {
			buyRoom = buyRoom.Sub(quantity.Sub(executed))
			localBid = decimal.Max(localBid, price)
		} else {
			sellRoom = sellRoom.Sub(quantity.Sub(executed))
			if localAsk.IsZero() || price.LessThan(localAsk) {
				localAsk = price
			}
		}
	}
	return buyRoom, sellRoom, localBid, localAsk, nil
}
