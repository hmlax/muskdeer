//go:build integration

package venue

import (
	"context"
	"errors"
	"testing"
	"time"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func TestExistingOuterBridgeUsesFirstSDKSlotForMissingBestPrice(t *testing.T) {
	runtime, cycle, client, _ := batchQuoteSetup(t.Context(), t, 40)
	fullSafeRefresh(cycle)
	var visible []*common.VenueOrder
	for _, order := range cycle.snapshot.Orders {
		if order.Side == string(domain.SideBuy) {
			visible = append(visible, order)
		}
	}
	visible = append(visible, &common.VenueOrder{
		OrderId:          "10000",
		ClientId:         native.InstanceClientPrefix(cycle.profile) + "outer-bridge",
		Side:             string(domain.SideSell),
		Price:            "110",
		Quantity:         "0.05",
		ExecutedQuantity: "0",
		Status:           native.StatusOpen,
	})
	cycle.snapshot.Orders = visible
	runtime.quoteBudget = newQuoteSendBudget(1)
	stop := errors.New("stop after the first actual SDK dispatch")
	client.firstError = stop
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := runtime.fillQuoteDepth(ctx, cycle); !errors.Is(err, stop) {
		t.Fatalf("first-slot SDK result=%v, want original error", err)
	}
	if client.calls.Load() != 1 || len(client.started) != 1 {
		t.Fatalf("first-slot failure replayed or sent later depth: calls=%d started=%d", client.calls.Load(), len(client.started))
	}
	request := <-client.started
	if request.Side != domain.SideSell || request.Price.String() != "101.1" {
		t.Fatalf("first SDK slot sent %s/%s rather than missing ask 101.1", request.Side, request.Price)
	}
}
