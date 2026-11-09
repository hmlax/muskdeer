package venue

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

// WashTrade checks memory settings/source only, then makes one SDK call.
// There are deliberately no database, batch, sequence, or receipt operations.
func (r *Runtime) WashTrade(ctx context.Context, input *pb.WashTradeRequest) (*common.SyntheticTapeResult, error) {
	if !rpcsecurity.HasService(ctx, rpcsecurity.ServiceStrategy) {
		return nil, status.Error(codes.PermissionDenied, native.MessageInvalid)
	}
	if r.closing.Load() || r.client.Load() == nil || r.cfg.Template != domain.StrategyTemplateWashTrade || r.cfg.InstanceID <= 0 || input.GetStrategyInstanceId() != r.cfg.InstanceID || input.GetStrategyRevision() != r.cfg.Revision {
		return nil, status.Error(codes.FailedPrecondition, native.MessageInvalid)
	}
	control := r.washControl.Load()
	if control == nil || !control.active || time.Since(control.observed) > washControlAge {
		return nil, status.Error(codes.FailedPrecondition, native.MessageInvalid)
	}
	target, err := native.WashTarget(r.cfg.Venue, input)
	if err != nil || target.GetPrice() != input.GetPrice() || target.GetQuantity() != input.GetQuantity() || target.GetSide() != input.GetSide() {
		return nil, status.Error(codes.InvalidArgument, native.MessageInvalid)
	}
	now := time.Now()
	previous := r.washNext.Load()
	if now.UnixMilli() < previous || !r.washNext.CompareAndSwap(previous, now.Add(r.cfg.Venue.SyntheticTape.Interval.AsDuration()).UnixMilli()) {
		return nil, status.Error(codes.ResourceExhausted, native.MessageInvalid)
	}
	until := input.GetBook().GetValidUntilMs()
	if input.GetTrade() != nil {
		until = input.GetTrade().GetValidUntilMs()
	}
	left := min(time.Until(time.UnixMilli(until))-50*time.Millisecond, r.cfg.OperationTimeout)
	if left <= 0 {
		return nil, status.Error(codes.InvalidArgument, native.MessageInvalid)
	}
	client, ok := r.client.Load().Client.(gateway.WashClient)
	if !ok {
		return nil, status.Error(codes.FailedPrecondition, native.MessageInvalid)
	}
	op, cancel := context.WithTimeout(ctx, left)
	defer cancel()
	response, sendErr := client.WashTrade(op, &gateway.SyntheticRequest{
		Symbol:   input.Symbol,
		Side:     domain.Side(input.Side),
		Price:    input.Price,
		Quantity: input.Quantity,
	})
	state := native.SyntheticPublished
	if sendErr != nil || !validWashReceipt(input, response) {
		state = native.SyntheticUnknown
	}
	result := &common.SyntheticTapeResult{
		State:       state,
		SourceTrade: proto.CloneOf(input.GetTrade()),
	}
	if response != nil {
		result.TradeId, result.Price, result.Quantity, result.TimestampMs = response.TradeID, response.Price, response.Quantity, response.TimestampMS
	}
	level := slog.LevelInfo
	if state == native.SyntheticUnknown {
		level = slog.LevelWarn
	}
	if err := r.logger.LogAttrs(ctx, level, state, slog.String("symbol", input.Symbol)); err != nil {
		return nil, err
	}
	return result, nil
}

func validWashReceipt(input *pb.WashTradeRequest, response *gateway.SyntheticResult) bool {
	if response.GetTradeID() == "" || response.GetTimestampMS() == 0 {
		return false
	}
	price, e1 := native.Value(response.Price, false, false)
	quantity, e2 := native.Value(response.Quantity, false, false)
	expectedPrice, e3 := native.Value(input.Price, false, false)
	expectedQty, e4 := native.Value(input.Quantity, false, false)
	return e1 == nil && e2 == nil && e3 == nil && e4 == nil && price.Equal(expectedPrice) && quantity.Equal(expectedQty)
}
