package venue

import (
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func TestQuoteRoomOnlyRetiresOwnUnsafeOrdersAtCapacity(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"full_unsafe", "bridge_free", "safe", "foreign", "last_order"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			cycle := fullQuoteTouchCycle(20)
			for _, order := range cycle.snapshot.Orders[:20] {
				order.Price = "101"
			}
			extra := proto.CloneOf(cycle.snapshot.Orders[0])
			extra.OrderId = "extra"
			cycle.snapshot.Orders = append(cycle.snapshot.Orders, extra)
			want := 0
			switch kind {
			case "full_unsafe":
				want = 2
			case "bridge_free":
				cycle.snapshot.Orders = cycle.snapshot.Orders[:40]
			case "safe":
				for _, order := range cycle.snapshot.Orders {
					if order.Side == string(domain.SideBuy) {
						order.Price = "99"
					}
				}
			case "foreign":
				for _, order := range cycle.snapshot.Orders {
					order.ClientId = "other_instance"
				}
			case "last_order":
				cycle.snapshot.Orders = cycle.snapshot.Orders[:1]
				cycle.targets = cycle.targets[:1]
				cycle.bridgeSlots = 0
			}
			wanted := make(map[string]*SubmitRequest)
			for _, target := range cycle.targets {
				price, _ := native.Value(target.Price, false, false)
				wanted[target.Side+"/"+price.String()] = &SubmitRequest{
					Side:  domain.Side(target.Side),
					Price: price,
				}
			}
			orders, err := quoteRoomRetirements(cycle, wanted)
			if err != nil || len(orders) != want {
				t.Fatalf("room(%s) orders=%d err=%v, want %d", kind, len(orders), err, want)
			}
		})
	}
}
