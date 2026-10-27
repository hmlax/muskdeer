package eventwire

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestFillWirePreservesFullLedgerProductAndRejectsJSONNumber(t *testing.T) {
	event := wireFillFixture()
	notice, err := Fill(t.Context(), event)
	if err != nil || !notice.Fact.QuoteNotional.Equal(decimal.RequireFromString(event.Fill.QuoteNotional)) || len(notice.PayloadHash) != 64 {
		t.Fatalf("typed ledger event = %+v, %v", notice, err)
	}
	data, err := protojson.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := strings.Replace(string(data), `"price":"`+event.Fill.Price+`"`, `"price":1`, 1)
	if corrupt == string(data) {
		t.Fatal("JSON number corruption fixture did not change the price")
	}
	var decoded commonv1.ExecutionEvent
	if err := protojson.Unmarshal([]byte(corrupt), &decoded); err == nil {
		t.Fatal("decimal JSON number was accepted as a string price")
	}
}

func TestFillWireRejectsMalformedOrAmbiguousFacts(t *testing.T) {
	for _, mutate := range []func(*commonv1.ExecutionEvent){
		func(e *commonv1.ExecutionEvent) { e.Fill.Price = "1e3" },
		func(e *commonv1.ExecutionEvent) { e.Fill.FeeAmount = "NaN" },
		func(e *commonv1.ExecutionEvent) { e.Fill.QuoteNotional = "1" },
		func(e *commonv1.ExecutionEvent) { e.Fill.Side = commonv1.OrderSide_ORDER_SIDE_UNSPECIFIED },
		func(e *commonv1.ExecutionEvent) { e.Fill.ExecutionVenue = string(domain.MarketSourceOKX) },
		func(e *commonv1.ExecutionEvent) { e.Intent = &commonv1.IntentEvent{} },
		func(e *commonv1.ExecutionEvent) { e.SchemaVersion++ },
		func(e *commonv1.ExecutionEvent) { e.AggregateId++ },
	} {
		event := proto.Clone(wireFillFixture()).(*commonv1.ExecutionEvent)
		mutate(event)
		if _, err := Fill(t.Context(), event); !errors.Is(err, ErrInvalidEvent) {
			t.Fatalf("invalid event was accepted: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Fill(ctx, wireFillFixture()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled decoder lost its cause: %v", err)
	}
}

func wireFillFixture() *commonv1.ExecutionEvent {
	price := "99999999999999999999.123456789012345678"
	value := decimal.RequireFromString(price)
	return &commonv1.ExecutionEvent{
		EventId:          "fill:simulator:1",
		EventType:        domain.EventTradeFilled,
		SchemaVersion:    domain.EventSchemaVersion,
		OccurredAtMs:     1,
		Producer:         domain.EventProducerExecution,
		AggregateId:      1,
		AggregateVersion: 1,
		Fill: &commonv1.FillEvent{
			TradeId:            "trade-1",
			ExecutionVenue:     domain.ExecutionVenueSimulator,
			CoreOrderId:        1,
			IntentId:           1,
			AccountId:          "account-1",
			StrategyInstanceId: 1,
			InstrumentId:       1,
			Side:               commonv1.OrderSide_ORDER_SIDE_BUY,
			Price:              price,
			Quantity:           price,
			QuoteNotional:      value.Mul(value).String(),
			FeeAmount:          "0",
			FeeAsset:           "USDT",
			SourceSequence:     1,
		},
	}
}
