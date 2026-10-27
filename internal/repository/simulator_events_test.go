package repository

import (
	"testing"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/eventwire"
)

func TestSimulatorFillEventBindsSourceAndPreservesExactLedgerValues(t *testing.T) {
	order := &modelv1.SimulatorOrder{
		Id:                 7,
		AccountId:          "event-account",
		StrategyInstanceId: 3,
		InstrumentId:       5,
		Side:               string(domain.SideBuy),
	}
	fill := &modelv1.SimulatorFill{
		Id:            11,
		CoreOrderId:   7,
		TradeId:       "event-trade",
		Price:         "100.123456789012345678",
		Quantity:      "0.000000000000000001",
		QuoteNotional: "0.000000000000000100123456789012345678",
		FeeAmount:     "0",
		FeeAsset:      "USDT",
	}
	event, err := simulatorFillEvent(t.Context(), order, fill, 9, 1)
	if err != nil {
		t.Fatal(err)
	}
	notice, err := eventwire.Fill(t.Context(), event)
	if err != nil || notice.Fact.IntentID != 9 || notice.Fact.SourceSequence != fill.Id || event.EventId != "fill:simulator:11" || event.Fill.QuoteNotional != fill.QuoteNotional {
		t.Fatalf("source fact changed at event boundary: %+v, %v", notice, err)
	}
	fill.CoreOrderId++
	if _, err := simulatorFillEvent(t.Context(), order, fill, 9, 1); err == nil {
		t.Fatal("fill event attached to a different core order")
	}
}
