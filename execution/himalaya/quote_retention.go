package himalaya

import (
	"strings"

	"github.com/shopspring/decimal"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

type quoteRetention struct {
	orders  map[string]bool
	targets map[string]bool
	pending bool
}

func retainQuoteOrders(profile *configv1.HimalayaProfile, snapshot *commonv1.HimalayaSnapshot, wanted map[string]*SubmitRequest, cutover bool) (*quoteRetention, error) {
	retained := &quoteRetention{
		orders:  make(map[string]bool),
		targets: make(map[string]bool),
	}
	for _, order := range snapshot.Orders {
		if !native.Active(order.Status) {
			continue
		}
		legacy := strings.HasPrefix(order.ClientId, native.LegacyPrefix+profile.Symbol+"_")
		if legacy && cutover || !legacy && !strings.HasPrefix(order.ClientId, native.ClientPrefix+profile.Symbol+"_") {
			return nil, native.ErrInvalid
		}
		key, quantity, err := orderKey(order)
		if err != nil {
			return nil, err
		}
		if target := wanted[key]; !legacy && target != nil && !retained.targets[key] && quantity.Equal(target.Quantity) {
			retained.orders[order.OrderId], retained.targets[key] = true, true
		}
	}
	if err := retainBridgeOrders(profile, snapshot, wanted, retained); err != nil {
		return nil, err
	}
	if err := retainRollingOrders(profile, snapshot, wanted, retained); err != nil {
		return nil, err
	}
	occupied := make(map[string]bool)
	for _, order := range snapshot.Orders {
		if !retained.orders[order.OrderId] {
			continue
		}
		key, quantity, err := orderKey(order)
		if err != nil {
			return nil, err
		}
		target := wanted[key]
		if occupied[key] || target == nil || !quantity.Equal(target.Quantity) {
			retained.pending = true
		}
		occupied[key] = true
		if target != nil {
			retained.targets[key] = true
		}
	}
	return retained, nil
}

// Keep one existing, non-crossing quote on a missing side until a replacement
// exists. Its original reservation remains held throughout cancellation proof.
func retainBridgeOrders(profile *configv1.HimalayaProfile, snapshot *commonv1.HimalayaSnapshot, wanted map[string]*SubmitRequest, retained *quoteRetention) error {
	limit, err := native.Value(profile.MaximumOrderNotional, false, false)
	if err != nil {
		return err
	}
	best := make(map[domain.Side]decimal.Decimal)
	for _, target := range wanted {
		previous := best[target.Side]
		if previous.IsZero() || target.Side == domain.SideBuy && target.Price.GreaterThan(previous) || target.Side == domain.SideSell && target.Price.LessThan(previous) {
			best[target.Side] = target.Price
		}
	}
	fresh := make(map[domain.Side]bool)
	for _, order := range snapshot.Orders {
		if retained.orders[order.OrderId] {
			fresh[domain.Side(order.Side)] = true
		}
	}
	bridges := make(map[domain.Side]*commonv1.HimalayaOrder)
	for _, order := range snapshot.Orders {
		side := domain.Side(order.Side)
		if fresh[side] || best[side].IsZero() || !native.Active(order.Status) || !strings.HasPrefix(order.ClientId, native.ClientPrefix+profile.Symbol+"_") {
			continue
		}
		price, ok := bridgePrice(order, best, limit)
		if !ok {
			continue
		}
		if previous := bridges[side]; previous != nil {
			oldPrice, _ := native.Value(previous.Price, false, false)
			if side == domain.SideBuy && !price.GreaterThan(oldPrice) || side == domain.SideSell && !price.LessThan(oldPrice) {
				continue
			}
		}
		bridges[side] = order
	}
	for _, order := range bridges {
		retained.orders[order.OrderId] = true
	}
	return nil
}

func bridgePrice(order *commonv1.HimalayaOrder, best map[domain.Side]decimal.Decimal, limit decimal.Decimal) (decimal.Decimal, bool) {
	price, e1 := native.Value(order.Price, false, false)
	_, quantity, e2 := orderKey(order)
	if e1 != nil || e2 != nil || !quantity.IsPositive() || price.Mul(quantity).GreaterThan(limit) {
		return decimal.Zero, false
	}
	switch domain.Side(order.Side) {
	case domain.SideBuy:
		return price, !best[domain.SideSell].IsZero() && price.LessThan(best[domain.SideSell])
	case domain.SideSell:
		return price, !best[domain.SideBuy].IsZero() && price.GreaterThan(best[domain.SideBuy])
	default:
		return decimal.Zero, false
	}
}

func quoteSubmissionOrder(targets []*commonv1.HimalayaTarget) []*commonv1.HimalayaTarget {
	buys := make([]*commonv1.HimalayaTarget, 0, len(targets))
	sells := make([]*commonv1.HimalayaTarget, 0, len(targets))
	for _, target := range targets {
		if target.Side == string(domain.SideBuy) {
			buys = append(buys, target)
		} else {
			sells = append(sells, target)
		}
	}
	ordered := make([]*commonv1.HimalayaTarget, 0, len(targets))
	for i := 0; i < max(len(buys), len(sells)); i++ {
		if i < len(buys) {
			ordered = append(ordered, buys[i])
		}
		if i < len(sells) {
			ordered = append(ordered, sells[i])
		}
	}
	return ordered
}
