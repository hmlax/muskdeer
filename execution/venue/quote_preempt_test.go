package venue

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func TestQuotePreemptionRequiresANewerFreshBestPrice(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"price", "quantity", "format", "expired", "reversed", "side_removed"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			plan := mailboxTestPlan(10)
			plan.Targets[0].Side = string(domain.SideBuy)
			plan.Targets = append(plan.Targets, &common.VenueTarget{
				Side:     string(domain.SideSell),
				Price:    "101",
				Quantity: "0.1",
			})
			next := proto.CloneOf(plan)
			next.Book.Sequence++
			want := false
			switch kind {
			case "price":
				next.Targets[0].Price = "99.1"
				want = true
			case "quantity":
				next.Targets[0].Quantity = "0.2"
			case "format":
				next.Targets[0].Price = "99.00"
			case "expired":
				next.Targets[0].Price = "99.1"
				next.Book.ValidUntilMs = time.Now().Add(-time.Second).UnixMilli()
			case "reversed":
				next.Targets[0].Price = "99.1"
				next.Book.Sequence = 9
			case "side_removed":
				next.Targets = next.Targets[:1]
				want = true
			}
			runtime := &Runtime{mailbox: newQuoteMailbox()}
			runtime.mailbox.offer(next)
			if got := runtime.quotePriceMoved(&quoteCycle{plan: plan}); got != want {
				t.Fatalf("preempt(%s)=%t want %t", kind, got, want)
			}
		})
	}
}
