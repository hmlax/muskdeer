package usdm

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/market/venue/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

// Book subscribers retain the latest pending observation. Trade subscribers
// below preserve a bounded burst and explicitly report overflow.
type subscriptions[T proto.Message] map[chan T]struct{}

// Trades preserve order within a bounded burst. Overflow terminates the
// subscriber explicitly instead of silently replacing earlier source events.
type tradeSubscriptions map[chan *common.VenueTrade]chan struct{}

func (s tradeSubscriptions) publish(trade *common.VenueTrade) {
	for ch, overflow := range s {
		select {
		case ch <- trade:
		default:
			close(overflow)
			delete(s, ch)
		}
	}
}

func (s subscriptions[T]) publish(value T) {
	for ch := range s {
		select {
		case ch <- value:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- value:
			default:
			}
		}
	}
}

func marketReader(ctx context.Context) bool {
	return rpcsecurity.HasService(ctx, rpcsecurity.ServiceStrategy) || rpcsecurity.HasService(ctx, rpcsecurity.ServiceExecution)
}

// SubscribeTrades streams fresh observations from the selected market source.
func (r *Runtime) SubscribeTrades(request *pb.TradeRequest, stream grpc.ServerStreamingServer[common.VenueTrade]) error {
	if r.children != nil {
		if child := r.children[request.GetSymbol()]; child != nil {
			return child.SubscribeTrades(request, stream)
		}
		return status.Error(codes.InvalidArgument, native.MessageInvalid)
	}
	if !marketReader(stream.Context()) {
		return status.Error(codes.PermissionDenied, native.MessageInvalid)
	}
	if !native.Symbol(request.GetSymbol()) {
		return status.Error(codes.InvalidArgument, native.MessageInvalid)
	}
	ch := make(chan *common.VenueTrade, native.WashSourceQueueCapacity)
	overflow := make(chan struct{})
	r.mu.Lock()
	if r.tradeReaders == nil {
		r.tradeReaders = make(tradeSubscriptions)
	}
	r.tradeReaders[ch] = overflow
	if latest := r.trades[request.Symbol]; latest.GetValidUntilMs() > time.Now().UnixMilli() {
		ch <- latest
	}
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.tradeReaders, ch); r.mu.Unlock() }()
	for {
		select {
		case <-overflow:
			return status.Error(codes.ResourceExhausted, native.MessageInvalid)
		default:
		}
		select {
		case <-overflow:
			return status.Error(codes.ResourceExhausted, native.MessageInvalid)
		case <-stream.Context().Done():
			return stream.Context().Err()
		case trade := <-ch:
			if trade.GetSymbol() != request.Symbol || trade.GetValidUntilMs() <= time.Now().UnixMilli() {
				continue
			}
			if err := stream.Send(proto.CloneOf(trade)); err != nil {
				return err
			}
		}
	}
}

// SubscribeBooks streams fresh depth with one pending frame per subscriber.
func (r *Runtime) SubscribeBooks(request *pb.BookRequest, stream grpc.ServerStreamingServer[common.VenueBook]) error {
	if r.children != nil {
		if child := r.children[request.GetSymbol()]; child != nil {
			return child.SubscribeBooks(request, stream)
		}
		return status.Error(codes.InvalidArgument, native.MessageInvalid)
	}
	if !marketReader(stream.Context()) {
		return status.Error(codes.PermissionDenied, native.MessageInvalid)
	}
	if !native.Symbol(request.GetSymbol()) {
		return status.Error(codes.InvalidArgument, native.MessageInvalid)
	}
	ch := make(chan *common.VenueBook, 1)
	r.mu.Lock()
	if r.bookReaders == nil {
		r.bookReaders = make(subscriptions[*common.VenueBook])
	}
	r.bookReaders[ch] = struct{}{}
	if latest := r.books[request.Symbol]; latest.GetValidUntilMs() > time.Now().UnixMilli() {
		ch <- latest
	}
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.bookReaders, ch); r.mu.Unlock() }()
	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case book := <-ch:
			if book.GetSymbol() != request.Symbol || book.GetValidUntilMs() <= time.Now().UnixMilli() {
				continue
			}
			if err := stream.Send(proto.CloneOf(book)); err != nil {
				return err
			}
		}
	}
}
