package venue

import (
	"context"
	"strconv"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

// These are diagnostic transport limits, unrelated to strategy instance counts.
const (
	maximumQuoteFlowReaders = 4
	maximumQuoteFlowQueue   = 128
	maximumQuoteFlowSymbols = 16
)

type quoteFlowReader struct {
	symbols  map[string]bool
	events   chan *pb.QuoteFlow
	overflow chan struct{}
	stopped  bool // guarded by hub.mu; the request retains its quota until it exits
}

// The publisher never waits for I/O, spawns work, or retains an order history.
// Each request owns one bounded queue until cancellation or explicit overflow.
type quoteFlowHub struct {
	mu      sync.Mutex
	readers map[*quoteFlowReader]struct{}
}

func newQuoteFlowHub() *quoteFlowHub {
	return &quoteFlowHub{readers: make(map[*quoteFlowReader]struct{})}
}

func (h *quoteFlowHub) subscribe(symbols []string) (*quoteFlowReader, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.readers) >= maximumQuoteFlowReaders {
		return nil, status.Error(codes.ResourceExhausted, native.MessageInvalid)
	}
	reader := &quoteFlowReader{
		symbols:  make(map[string]bool, len(symbols)),
		events:   make(chan *pb.QuoteFlow, maximumQuoteFlowQueue),
		overflow: make(chan struct{}),
	}
	for _, symbol := range symbols {
		reader.symbols[symbol] = true
	}
	h.readers[reader] = struct{}{}
	return reader, nil
}

func (h *quoteFlowHub) unsubscribe(reader *quoteFlowReader) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.readers, reader)
}

func (h *quoteFlowHub) interested(symbol string) bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for reader := range h.readers {
		if !reader.stopped && reader.symbols[symbol] {
			return true
		}
	}
	return false
}

func (h *quoteFlowHub) publish(flow *pb.QuoteFlow) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for reader := range h.readers {
		if reader.stopped || !reader.symbols[flow.Symbol] {
			continue
		}
		select {
		case reader.events <- proto.CloneOf(flow):
		default:
			reader.stopped = true
			close(reader.overflow) // Only the publisher closes this signal, once.
		}
	}
}

// SubscribeQuoteFlows is read-only and available only to the authenticated
// strategy service. It does not subscribe to or change any financial state.
func (r *Instances) SubscribeQuoteFlows(request *pb.QuoteFlowRequest, stream grpc.ServerStreamingServer[pb.QuoteFlow]) error {
	if !rpcsecurity.HasService(stream.Context(), rpcsecurity.ServiceStrategy) {
		return status.Error(codes.PermissionDenied, native.MessageInvalid)
	}
	if len(request.GetSymbols()) == 0 || len(request.Symbols) > maximumQuoteFlowSymbols || r.quoteFlows == nil {
		return status.Error(codes.InvalidArgument, native.MessageInvalid)
	}
	seen := make(map[string]bool, len(request.Symbols))
	for _, symbol := range request.Symbols {
		known := false
		for _, child := range r.ordered {
			if child.directMaker() && len(child.cfg.Venue.Profiles) == 1 && child.cfg.Venue.Profiles[0].Symbol == symbol {
				known = true
				break
			}
		}
		if !known || seen[symbol] {
			return status.Error(codes.InvalidArgument, native.MessageInvalid)
		}
		seen[symbol] = true
	}
	if err := stream.Context().Err(); err != nil {
		return status.FromContextError(err).Err()
	}
	reader, err := r.quoteFlows.subscribe(request.Symbols)
	if err != nil {
		return err
	}
	defer r.quoteFlows.unsubscribe(reader)
	if err := stream.SendHeader(metadata.MD{}); err != nil {
		return err
	}
	for {
		select {
		case <-reader.overflow:
			return status.Error(codes.ResourceExhausted, native.MessageInvalid)
		default:
		}
		select {
		case <-stream.Context().Done():
			return status.FromContextError(stream.Context().Err()).Err()
		case <-reader.overflow:
			return status.Error(codes.ResourceExhausted, native.MessageInvalid)
		case flow := <-reader.events:
			if err := stream.Send(flow); err != nil {
				return err
			}
		}
	}
}

func (r *Runtime) publishQuoteFlow(ctx context.Context, cycle *quoteCycle, request gateway.QuoteRequest, result *gateway.SubmitResult, started, finished time.Time, failure error) {
	span := trace.SpanFromContext(ctx)
	interested := r.quoteFlows.interested(request.Scope.Symbol)
	if !interested && !span.IsRecording() {
		return
	}
	book, plan := cycle.plan.GetBook(), cycle.plan
	flow := &pb.QuoteFlow{
		TargetId:                  plan.GetRequestId(),
		ClientId:                  request.ClientID,
		Symbol:                    request.Scope.Symbol,
		Side:                      string(request.Side),
		Price:                     request.Price.String(),
		SourceSequence:            book.GetSequence(),
		SourceEventAtMs:           book.GetEventAtMs(),
		SourceReceivedAtMs:        book.GetReceivedAtMs(),
		SourceSnapshotStartedAtMs: book.GetSnapshotStartedAtMs(),
		SourceValidUntilMs:        book.GetValidUntilMs(),
		ProposedAtUnixNano:        plan.GetProposedAtUnixNano(),
		AcceptedAtUnixNano:        plan.GetAcceptedAtUnixNano(),
		SdkStartedAtUnixNano:      started.UnixNano(),
		SdkFinishedAtUnixNano:     finished.UnixNano(),
		Acknowledged:              failure == nil && result != nil && result.OrderID != 0,
		Reason:                    safeReason(failure),
		StrategyInstanceId:        r.cfg.InstanceID,
		Source:                    book.GetSource(),
		Segment:                   book.GetSegment(),
	}
	if !cycle.startedAt.IsZero() {
		flow.TurnStartedAtUnixNano = cycle.startedAt.UnixNano()
	}
	if permit := quoteTurnPermitFrom(ctx); permit != nil {
		flow.PreparationWaitNano = int64(permit.waited)
	}
	if result != nil && result.OrderID != 0 {
		flow.OrderId = strconv.FormatUint(result.OrderID, 10)
	}
	if span.IsRecording() {
		span.AddEvent("quote.flow", trace.WithTimestamp(finished), trace.WithAttributes(
			attribute.String("quote.target.id", flow.TargetId),
			attribute.String("quote.client.id", flow.ClientId),
			attribute.String("quote.order.id", flow.OrderId),
			attribute.String("market.symbol", flow.Symbol),
			attribute.Int64("market.source.sequence", flow.SourceSequence),
			attribute.Int64("market.source.event_at_ms", flow.SourceEventAtMs),
			attribute.Int64("market.source.received_at_ms", flow.SourceReceivedAtMs),
			attribute.Int64("quote.sdk.started_at_unix_nano", flow.SdkStartedAtUnixNano),
			attribute.Int64("quote.sdk.finished_at_unix_nano", flow.SdkFinishedAtUnixNano),
			attribute.Bool("quote.acknowledged", flow.Acknowledged),
			attribute.String("quote.reason", flow.Reason)))
	}
	if interested {
		r.quoteFlows.publish(flow)
	}
}
