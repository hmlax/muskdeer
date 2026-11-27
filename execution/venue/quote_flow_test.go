package venue

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

func TestQuoteFlowQueueIsScopedCopiedAndReportsOverflow(t *testing.T) {
	hub := newQuoteFlowHub()
	first, err := hub.subscribe([]string{"BTCUSDT"})
	if err != nil {
		t.Fatal(err)
	}
	defer hub.unsubscribe(first)
	second, err := hub.subscribe([]string{"ETHUSDT"})
	if err != nil {
		t.Fatal(err)
	}
	defer hub.unsubscribe(second)
	flow := &pb.QuoteFlow{
		Symbol:  "BTCUSDT",
		OrderId: "42",
	}
	hub.publish(flow)
	flow.OrderId = "mutated"
	if got := <-first.events; got.OrderId != "42" || len(second.events) != 0 {
		t.Fatal("diagnostic payload aliasing or cross-symbol delivery")
	}
	for range maximumQuoteFlowQueue + 1 {
		hub.publish(flow)
	}
	select {
	case <-first.overflow:
	default:
		t.Fatal("slow subscriber did not receive explicit loss notification")
	}
	if hub.interested("BTCUSDT") || !hub.interested("ETHUSDT") {
		t.Fatal("overflow kept an abandoned queue or removed another reader")
	}
	for range maximumQuoteFlowQueue * 2 {
		hub.publish(flow) // Must not block the trading caller behind a slow reader.
	}
}

func TestQuoteFlowSubscriptionIsBoundedAndCleansUp(t *testing.T) {
	hub := newQuoteFlowHub()
	readers := make([]*quoteFlowReader, 0, maximumQuoteFlowReaders)
	for range maximumQuoteFlowReaders {
		reader, err := hub.subscribe([]string{"BTCUSDT"})
		if err != nil {
			t.Fatal(err)
		}
		readers = append(readers, reader)
	}
	if _, err := hub.subscribe([]string{"BTCUSDT"}); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("unbounded diagnostic readers: %v", err)
	}
	for range maximumQuoteFlowQueue + 1 {
		hub.publish(&pb.QuoteFlow{Symbol: "BTCUSDT"})
	}
	if _, err := hub.subscribe([]string{"BTCUSDT"}); status.Code(err) != codes.ResourceExhausted {
		t.Fatal("overflow released the quota of a possibly blocked stream before it exited")
	}
	for _, reader := range readers {
		hub.unsubscribe(reader)
		hub.unsubscribe(reader)
	}
	if hub.interested("BTCUSDT") {
		t.Fatal("canceled readers retained transport queues")
	}
	if _, err := hub.subscribe([]string{"BTCUSDT"}); err != nil {
		t.Fatal(err)
	}
}

type quoteFlowTestStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *quoteFlowTestStream) Context() context.Context { return s.ctx }
func (s *quoteFlowTestStream) Send(*pb.QuoteFlow) error { return nil }

func TestQuoteFlowRPCRequiresIdentityAndExplicitConfiguredSymbols(t *testing.T) {
	runtime, _, _ := makerObservationRuntime()
	instances := &Instances{
		quoteFlows: newQuoteFlowHub(),
		ordered:    []*Runtime{runtime},
	}
	for _, kind := range []string{"unauthenticated", "empty", "unknown", "duplicate", "too_many", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(washTestContext(t.Context()))
			defer cancel()
			request := &pb.QuoteFlowRequest{Symbols: []string{"BTCUSDT"}}
			want := codes.InvalidArgument
			switch kind {
			case "unauthenticated":
				ctx, want = t.Context(), codes.PermissionDenied
			case "empty":
				request.Symbols = nil
			case "unknown":
				request.Symbols = []string{"UNKNOWN"}
			case "duplicate":
				request.Symbols = []string{"BTCUSDT", "BTCUSDT"}
			case "too_many":
				request.Symbols = make([]string, maximumQuoteFlowSymbols+1)
			case "canceled":
				cancel()
				want = codes.Canceled
			}
			err := instances.SubscribeQuoteFlows(request, &quoteFlowTestStream{ctx: ctx})
			if status.Code(err) != want || instances.quoteFlows.interested("BTCUSDT") {
				t.Fatalf("subscription(%s)=%v; retained reader=%t", kind, err, instances.quoteFlows.interested("BTCUSDT"))
			}
		})
	}
}

type quoteFlowAuthenticatedStream struct{ grpc.ServerStream }

func (s quoteFlowAuthenticatedStream) Context() context.Context {
	return washTestContext(s.ServerStream.Context())
}

func TestQuoteFlowGeneratedRPCPreservesIdentityAndCancellation(t *testing.T) {
	runtime, _, _ := makerObservationRuntime()
	instances := &Instances{
		quoteFlows: newQuoteFlowHub(),
		ordered:    []*Runtime{runtime},
	}
	listener := bufconn.Listen(64 * 1024)
	server := grpc.NewServer(grpc.StreamInterceptor(func(srv any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, next grpc.StreamHandler) error {
		return next(srv, quoteFlowAuthenticatedStream{stream})
	}))
	pb.RegisterExecutionServer(server, instances)
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	conn, err := grpc.NewClient("passthrough:///quote-flow", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return listener.DialContext(ctx)
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	stream, err := pb.NewExecutionClient(conn).SubscribeQuoteFlows(ctx, &pb.QuoteFlowRequest{Symbols: []string{"BTCUSDT"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Header(); err != nil {
		t.Fatal(err)
	}
	want := &pb.QuoteFlow{
		Symbol:               "BTCUSDT",
		TargetId:             "selected-after-observation",
		ClientId:             "fresh-independent-client",
		OrderId:              "18446744073709551615",
		Price:                "0.000000000000000001",
		SourceSequence:       123456789,
		SourceEventAtMs:      1700000000000,
		SdkStartedAtUnixNano: 1700000000000000001,
		Acknowledged:         false,
		Reason:               reasonOutcomeUnknown,
	}
	instances.quoteFlows.publish(want)
	got, err := stream.Recv()
	if err != nil || !proto.Equal(got, want) {
		t.Fatalf("typed diagnostic stream=%v, %v", got, err)
	}
	cancel()
	if _, err := stream.Recv(); status.Code(err) != codes.Canceled {
		t.Fatalf("stream cancellation=%v", err)
	}
}

func TestQuoteFlowConcurrentPublishAndReaderRetirement(t *testing.T) {
	hub := newQuoteFlowHub()
	var group sync.WaitGroup
	for range maximumQuoteFlowReaders {
		group.Go(func() {
			for range 100 {
				reader, err := hub.subscribe([]string{"BTCUSDT"})
				if err == nil {
					hub.unsubscribe(reader)
				}
			}
		})
	}
	group.Go(func() {
		for range 1000 {
			hub.publish(&pb.QuoteFlow{Symbol: "BTCUSDT"})
		}
	})
	group.Wait()
	if hub.interested("BTCUSDT") {
		t.Fatal("concurrent canceled reader was retained")
	}
}

func TestQuoteFlowUsesPricingSourceInsteadOfNewerGuardAndKeepsUnknown(t *testing.T) {
	runtime, _, _ := makerObservationRuntime()
	runtime.quoteFlows = newQuoteFlowHub()
	reader, err := runtime.quoteFlows.subscribe([]string{"BTCUSDT"})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.quoteFlows.unsubscribe(reader)
	accepted := time.Now().UnixNano()
	cycle := &quoteCycle{
		startedAt: time.Now(),
		plan: &pb.QuotePlan{
			RequestId: "actual-retargeted-plan",
			Book: &common.VenueBook{
				Sequence:     103,
				EventAtMs:    1700000000000,
				ReceivedAtMs: 1700000000007,
			},
			AcceptedAtUnixNano: accepted,
		},
		authority: LiveAuthority{Book: &common.VenueBook{Sequence: 104}},
	}
	request := gateway.QuoteRequest{
		Scope:    gateway.Scope{Symbol: "BTCUSDT"},
		ClientID: "actual-once-only-client",
		Side:     domain.SideBuy,
		Price:    decimal.RequireFromString("0.000000000000000001"),
	}
	for _, failure := range []error{nil, domain.ErrOutcomeUnknown} {
		started, finished := time.Now(), time.Now()
		runtime.publishQuoteFlow(t.Context(), cycle, request, &gateway.SubmitResult{OrderID: 42}, started, finished, failure)
		got := <-reader.events
		if got.TargetId != cycle.plan.RequestId || got.SourceSequence != 103 || got.SourceReceivedAtMs != cycle.plan.Book.ReceivedAtMs || got.ClientId != request.ClientID || got.OrderId != "42" || got.AcceptedAtUnixNano != accepted || got.SdkStartedAtUnixNano != started.UnixNano() || got.SdkFinishedAtUnixNano != finished.UnixNano() || got.Price != request.Price.String() {
			t.Fatalf("actual target, decimals or transport identity lost: %v", got)
		}
		if got.Acknowledged != (failure == nil) || failure != nil && got.Reason != reasonOutcomeUnknown {
			t.Fatal("unknown outcome was reported as an acknowledged order")
		}
	}
}

func TestQuoteMailboxOwnsAcceptanceTimestampWithoutChangingSourceIdentity(t *testing.T) {
	mailbox := newQuoteMailbox()
	plan := mailboxTestPlan(103)
	plan.RequestId = "same-target-across-rpc-and-writer"
	plan.ProposedAtUnixNano = 17
	plan.AcceptedAtUnixNano = 1
	before := time.Now().UnixNano()
	mailbox.offer(plan)
	got := mailbox.pending.Load()
	if got.AcceptedAtUnixNano < before || got.AcceptedAtUnixNano > time.Now().UnixNano() || plan.AcceptedAtUnixNano != 1 || got.ProposedAtUnixNano != 17 || got.RequestId != plan.RequestId || !proto.Equal(got.Book, plan.Book) {
		t.Fatal("telemetry trusted a caller acceptance time, mutated the caller, or changed source identity")
	}
}
