package venue

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

// WashTrade checks memory settings/source, then requests one SDK publication.
// The adapter filters events that do not satisfy current venue market rules.
// There are deliberately no database, batch, sequence, or receipt operations.
func (r *Runtime) WashTrade(ctx context.Context, input *pb.WashTradeRequest) (*common.SyntheticTapeResult, error) {
	client, err := r.currentWashClient(ctx, input.GetStrategyInstanceId(), input.GetStrategyRevision(), input.GetSymbol())
	if err != nil {
		return nil, err
	}
	base, err := native.WashTarget(r.cfg.Venue, input)
	if err != nil || base.GetPrice() != input.GetPrice() || base.GetSide() != input.GetSide() {
		return nil, status.Error(codes.InvalidArgument, native.MessageInvalid)
	}
	until := input.GetBook().GetValidUntilMs()
	if input.GetTrade() != nil {
		until = input.GetTrade().GetValidUntilMs()
	}
	left := min(time.Until(time.UnixMilli(until))-50*time.Millisecond, r.cfg.OperationTimeout)
	if left <= 0 {
		return nil, status.Error(codes.InvalidArgument, native.MessageInvalid)
	}
	op, cancel := context.WithTimeout(ctx, left)
	defer cancel()
	rules, err := client.WashMarket(op, input.Symbol)
	if err != nil || rules == nil || !time.Now().Before(rules.ValidUntil) {
		return nil, status.Error(codes.FailedPrecondition, native.MessageInvalid)
	}
	target, err := native.WashTargetForMarket(r.cfg.Venue, input, rules.Market)
	if errors.Is(err, native.ErrWashMinimumAboveLimit) {
		return nil, status.Error(codes.FailedPrecondition, native.MessageWashMinimumAboveLimit)
	}
	if err != nil || target.GetQuantity() != input.Quantity {
		return nil, status.Error(codes.InvalidArgument, native.MessageInvalid)
	}
	if err := op.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	if err := r.admitWashFrequency(input, time.Now()); err != nil {
		return nil, err
	}
	response, sendErr := client.WashTrade(op, &gateway.SyntheticRequest{
		Symbol:   input.Symbol,
		Side:     domain.Side(input.Side),
		Price:    input.Price,
		Quantity: input.Quantity,
	})
	if errors.Is(sendErr, gateway.ErrBelowMarketMinimum) {
		return &common.SyntheticTapeResult{
			State:       native.SyntheticSkippedMinimum,
			SourceTrade: proto.CloneOf(input.GetTrade()),
		}, nil
	}
	if errors.Is(sendErr, gateway.ErrNotDispatched) {
		return nil, status.Error(codes.FailedPrecondition, native.MessageInvalid)
	}
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
	if err := r.logger.LogAttrs(ctx, level, state, slog.String("symbol", input.Symbol), slog.String("price", input.Price), slog.String("quantity", input.Quantity), slog.Bool("minimum_adjusted", target.Quantity != base.Quantity)); err != nil {
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
