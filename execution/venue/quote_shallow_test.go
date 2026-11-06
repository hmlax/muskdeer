package venue

import (
	"strconv"
	"testing"

	"github.com/shopspring/decimal"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func TestRollingQuotesFillShallowSidesBeforeRetiringSafeDepth(t *testing.T) {
	t.Parallel()
	profile := &commonv1.VenueProfilePolicy{
		Symbol:               "BTCUSDT",
		MaximumOrderNotional: "500",
	}
	for _, count := range []int{2, 6, 17, 18, 19, 20} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			t.Parallel()
			snapshot := &commonv1.VenueSnapshot{}
			wanted := make(map[string]*SubmitRequest)
			for index, side := range []domain.Side{domain.SideBuy, domain.SideSell} {
				for level := range 20 {
					distance := decimal.NewFromInt(int64(level + 1)).Shift(-1)
					price := decimal.NewFromInt(100).Sub(distance)
					if side == domain.SideSell {
						price = decimal.NewFromInt(100).Add(distance)
					}
					wanted[string(side)+"/"+price.String()] = &SubmitRequest{
						Side:     side,
						Price:    price,
						Quantity: decimal.NewFromInt(2),
					}
					if level < count {
						snapshot.Orders = append(snapshot.Orders, retentionOrder(strconv.Itoa(index*20+level+1), side, price.String()))
					}
				}
			}
			retained, err := retainQuoteOrders(profile, snapshot, wanted, true, decimal.RequireFromString("0.1"))
			if err != nil {
				t.Fatal(err)
			}
			kept := make(map[string]int)
			for _, order := range snapshot.Orders {
				if retained.orders[order.OrderId] {
					kept[order.Side]++
				}
			}
			want := min(count, 18)
			if kept[string(domain.SideBuy)] != want || kept[string(domain.SideSell)] != want {
				t.Errorf("retainQuoteOrders(%d levels per side) = %v, want %d safe levels retained on each side before filling", count, kept, want)
			}
		})
	}
}
