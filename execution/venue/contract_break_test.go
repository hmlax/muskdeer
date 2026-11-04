package venue

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	executionv1 "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	marketv1 "github.com/hmlax/muskdeer/deer/pb/market/venue/v1"
	portfoliov1 "github.com/hmlax/muskdeer/deer/pb/portfolio/venue/v1"
)

type contractPeer struct {
	executionv1.UnimplementedExecutionServer
	placements atomic.Uint32
	tape       atomic.Uint32
}

func (s *contractPeer) Reconcile(context.Context, *executionv1.QuotePlan) (*executionv1.QuoteResult, error) {
	s.placements.Add(1)
	return &executionv1.QuoteResult{}, nil
}

func (s *contractPeer) PublishSynthetic(context.Context, *commonv1.SyntheticTapePlan) (*commonv1.SyntheticTapeResult, error) {
	s.tape.Add(1)
	return &commonv1.SyntheticTapeResult{}, nil
}

func TestVenueContractsUseServiceNamespacesAndRejectRetiredRPCs(t *testing.T) {
	for _, pair := range [][2]string{
		{executionv1.Execution_ServiceDesc.ServiceName, "muskdeer.execution.venue.v1.Execution"},
		{marketv1.Market_ServiceDesc.ServiceName, "muskdeer.market.venue.v1.Market"},
		{portfoliov1.Portfolio_ServiceDesc.ServiceName, "muskdeer.portfolio.venue.v1.Portfolio"},
	} {
		if pair[0] != pair[1] {
			t.Fatalf("service namespace = %q, want %q", pair[0], pair[1])
		}
	}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	peer := &contractPeer{}
	executionv1.RegisterExecutionServer(server, peer)
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		if err := listener.Close(); err != nil {
			t.Error(err)
		}
		if err := <-served; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Error(err)
		}
	})
	connection, err := grpc.NewClient("passthrough:///venue-contract", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := connection.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := connection.Invoke(ctx, "/muskdeer.himalaya.v1.Execution/Reconcile", &executionv1.QuotePlan{}, &executionv1.QuoteResult{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("retired reconciliation RPC = %v, want Unimplemented", err)
	}
	if err := connection.Invoke(ctx, "/muskdeer.himalaya.v1.Execution/PublishSynthetic", &commonv1.SyntheticTapePlan{}, &commonv1.SyntheticTapeResult{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("retired tape RPC = %v, want Unimplemented", err)
	}
	if peer.placements.Load() != 0 || peer.tape.Load() != 0 {
		t.Fatal("retired RPC triggered side effects")
	}
	client := executionv1.NewExecutionClient(connection)
	if _, err := client.Reconcile(ctx, &executionv1.QuotePlan{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.PublishSynthetic(ctx, &commonv1.SyntheticTapePlan{}); err != nil {
		t.Fatal(err)
	}
	if peer.placements.Load() != 1 || peer.tape.Load() != 1 {
		t.Fatal("new RPC did not dispatch exactly once")
	}
}

func TestRetiredProviderMessagesAreNotRegistered(t *testing.T) {
	for _, name := range []protoreflect.FullName{"muskdeer.common.v1.HimalayaScope", "muskdeer.common.v1.HimalayaAuthority", "muskdeer.config.v1.HimalayaRuntime"} {
		if _, err := protoregistry.GlobalTypes.FindMessageByName(name); !errors.Is(err, protoregistry.NotFound) {
			t.Fatalf("retired message %s is still registered: %v", name, err)
		}
	}
}
