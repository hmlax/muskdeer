package native

import (
	"context"
	"testing"
	"time"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	values "github.com/hmlax/muskdeer/deer/trading/native"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

type minimumExecution struct {
	pb.ExecutionClient
	rules  *pb.WashMarketRules
	reads  int
	writes []*pb.WashTradeRequest
}

func (c *minimumExecution) GetWashMarket(context.Context, *pb.WashMarketRequest, ...grpc.CallOption) (*pb.WashMarketRules, error) {
	c.reads++
	return proto.CloneOf(c.rules), nil
}
func (c *minimumExecution) WashTrade(_ context.Context, input *pb.WashTradeRequest, _ ...grpc.CallOption) (*common.SyntheticTapeResult, error) {
	c.writes = append(c.writes, proto.CloneOf(input))
	return &common.SyntheticTapeResult{State: values.SyntheticPublished}, nil
}
func TestWashStrategyRaisesCalculatedQuantityBeforeSendingAndRefreshesRules(t *testing.T) {
	cfg := sourceWashConfig()
	client := &minimumExecution{rules: &pb.WashMarketRules{
		Market: &common.VenueMarket{
			PriceTick:       "0.1",
			QuantityStep:    "0.001",
			MinimumQuantity: "0.001",
			MaximumQuantity: "1",
			MinimumNotional: "0.5",
		},
		ValidUntilMs: time.Now().Add(time.Second).UnixMilli(),
	}}
	sender := &washSender{cfg: cfg}
	input := &pb.WashTradeRequest{
		StrategyInstanceId: cfg.InstanceID,
		StrategyRevision:   cfg.Revision,
		Symbol:             "BTCUSDT",
		Source:             &pb.WashTradeRequest_Trade{Trade: sourceWashTrade("42")},
	}
	input.GetTrade().Quantity = "0.001"
	for range 2 {
		if _, err := sender.send(t.Context(), client, input); err != nil {
			t.Fatal(err)
		}
	}
	if client.reads != 1 || len(client.writes) != 2 || client.writes[0].Quantity != "0.005" || input.Quantity != "" || input.GetTrade().Quantity != "0.001" {
		t.Fatal("strategy floor, rule cache or original observation changed")
	}
	sender.rules.ValidUntilMs = time.Now().Add(-time.Second).UnixMilli()
	client.rules.Market.MinimumNotional = "0.6"
	if _, err := sender.send(t.Context(), client, input); err != nil {
		t.Fatal(err)
	}
	if client.reads != 2 || client.writes[2].Quantity != "0.006" {
		t.Fatal("strategy used an expired venue minimum")
	}
}

func TestWashStrategyKeepsSufficientQuantityAndDoesNotWriteAboveCap(t *testing.T) {
	cfg := sourceWashConfig()
	client := &minimumExecution{rules: &pb.WashMarketRules{
		Market: &common.VenueMarket{
			PriceTick:       "0.1",
			QuantityStep:    "0.001",
			MinimumQuantity: "0.001",
			MaximumQuantity: "1",
			MinimumNotional: "0.5",
		},
		ValidUntilMs: time.Now().Add(time.Second).UnixMilli(),
	}}
	sender := &washSender{cfg: cfg}
	input := &pb.WashTradeRequest{
		Symbol: "BTCUSDT",
		Source: &pb.WashTradeRequest_Trade{Trade: sourceWashTrade("42")},
	}
	if _, err := sender.send(t.Context(), client, input); err != nil {
		t.Fatal(err)
	}
	if client.writes[0].Quantity != "0.008" {
		t.Fatal("sufficient source quantity changed")
	}
	cfg.Venue.SyntheticTape.Profiles[0].Quantity = "0.001"
	if _, err := sender.send(t.Context(), client, input); err != values.ErrWashMinimumAboveLimit || len(client.writes) != 1 {
		t.Fatalf("incompatible cap dispatched or failed ambiguously: %v", err)
	}
}
