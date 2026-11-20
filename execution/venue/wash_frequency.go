package venue

import (
	"time"

	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (r *Runtime) admitWashFrequency(input *pb.WashTradeRequest, now time.Time) error {
	if native.WashFollowsSource(r.cfg.Venue) {
		id, err := native.TradeSequence(input.GetTrade())
		if err != nil {
			return status.Error(codes.InvalidArgument, native.MessageInvalid)
		}
		for {
			previous := r.washSource.Load()
			if id <= previous {
				return status.Error(codes.AlreadyExists, native.MessageInvalid)
			}
			if r.washSource.CompareAndSwap(previous, id) {
				return nil
			}
		}
	}
	previous := r.washNext.Load()
	if now.UnixMilli() < previous || !r.washNext.CompareAndSwap(previous, now.Add(r.cfg.Venue.SyntheticTape.Interval.AsDuration()).UnixMilli()) {
		return status.Error(codes.ResourceExhausted, native.MessageInvalid)
	}
	return nil
}
