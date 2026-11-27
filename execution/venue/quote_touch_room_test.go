package venue

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"

	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func fullSafeBridgeCycle() (*quoteCycle, map[string]*SubmitRequest) {
	cycle := fullQuoteTouchCycle(20)
	for _, index := range []int{19, 39} {
		order := proto.CloneOf(cycle.snapshot.Orders[index])
		order.OrderId += "-bridge"
		price := decimal.RequireFromString(order.Price)
		if order.Side == string(domain.SideBuy) {
			price = price.Sub(decimal.RequireFromString("0.1"))
		} else {
			price = price.Add(decimal.RequireFromString("0.1"))
		}
		order.Price = price.String()
		cycle.snapshot.Orders = append(cycle.snapshot.Orders, order)
	}
	wanted := make(map[string]*SubmitRequest)
	for _, target := range cycle.targets {
		price := decimal.RequireFromString(target.Price)
		wanted[target.Side+"/"+price.String()] = &SubmitRequest{
			Side:     domain.Side(target.Side),
			Price:    price,
			Quantity: decimal.RequireFromString(target.Quantity),
		}
	}
	return cycle, wanted
}

func TestMovingSourceCannotStarveBestPriceBehindFullSafeBridge(t *testing.T) {
	t.Parallel()
	cycle, wanted := fullSafeBridgeCycle()
	cycle.plan = mailboxTestPlan(10)
	cycle.plan.Targets = cycle.targets
	cycle.snapshot.ValidUntilMs = time.Now().Add(native.SnapshotAge).UnixMilli()
	next := proto.CloneOf(cycle.plan)
	next.Book.Sequence++
	next.Targets[0].Price = "100"
	runtime := &Runtime{mailbox: newQuoteMailbox()}
	runtime.mailbox.offer(next)
	deferred, err := runtime.deferQuoteRetirement(cycle)
	if err != nil || !deferred || cycle.canPlace(cycle.targets[0]) || cycle.canPlace(cycle.targets[20]) {
		t.Fatalf("fixture does not reproduce blocked touches with safe retirement deferred: deferred=%t err=%v", deferred, err)
	}
	orders, err := quoteRoomRetirements(cycle, wanted)
	if err != nil || len(orders) != 2 {
		t.Fatalf("full safe bridge prevents both best-price replacements: room=%d err=%v, want one per side", len(orders), err)
	}
	for _, order := range orders {
		want := "96.9"
		if order.Side == string(domain.SideSell) {
			want = "103.1"
		}
		if order.Price != want {
			t.Fatalf("room retired %s rather than the farthest obsolete %s", order.Price, want)
		}
	}
}

func TestTouchRoomPreservesExistingDepthOwnershipAndBetterPrices(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"free_bridge", "touch_visible", "better_price", "foreign_capacity", "duplicate", "pending_capacity"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			cycle, wanted := fullSafeBridgeCycle()
			want := 0
			switch kind {
			case "free_bridge":
				cycle.snapshot.Orders = cycle.snapshot.Orders[:40]
			case "touch_visible":
				cycle.snapshot.Orders[0].Price = cycle.targets[0].Price
				cycle.snapshot.Orders[20].Price = cycle.targets[20].Price
			case "better_price":
				cycle.snapshot.Orders[0].Price = "100"
				cycle.snapshot.Orders[20].Price = "100"
			case "foreign_capacity":
				for _, order := range cycle.snapshot.Orders {
					order.ClientId = "other_instance"
				}
			case "duplicate":
				for _, order := range cycle.snapshot.Orders {
					order.Price = "98.9"
					if order.Side == string(domain.SideSell) {
						order.Price = "101.1"
					}
				}
				want = 2
			case "pending_capacity":
				cycle.cancelling = map[string]bool{
					cycle.snapshot.Orders[40].OrderId: true,
					cycle.snapshot.Orders[41].OrderId: true,
				}
				want = 2 // Recheck the existing pending subset, without retiring more safe depth.
			}
			orders, err := quoteRoomRetirements(cycle, wanted)
			if err != nil || len(orders) != want {
				t.Fatalf("touch room(%s)=%d err=%v, want %d", kind, len(orders), err, want)
			}
			for _, order := range orders {
				if kind == "pending_capacity" && !cycle.cancelling[order.OrderId] {
					t.Fatal("an existing pending cancellation caused extra safe depth to retire")
				}
			}
		})
	}
}
