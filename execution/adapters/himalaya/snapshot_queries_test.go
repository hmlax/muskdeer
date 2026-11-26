package himalaya

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	pb "gitlab.com/himalaya/lukla-go/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type snapshotQueryPeer struct {
	sdkPeer
	marketStarted  chan struct{}
	accountStarted chan struct{}
	accountStopped chan struct{}
	failMarket     bool
	ordersRead     atomic.Bool
	positionsRead  atomic.Bool
}

func (p *snapshotQueryPeer) GetMarket(ctx context.Context, request *pb.GetMarketRequest) (*pb.GetMarketResponse, error) {
	close(p.marketStarted)
	select {
	case <-p.accountStarted:
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	if p.failMarket {
		return nil, status.Error(codes.Unavailable, "market read failed")
	}
	return p.sdkPeer.GetMarket(ctx, request)
}

func (p *snapshotQueryPeer) GetAccount(ctx context.Context, request *pb.GetAccountRequest) (*pb.GetAccountResponse, error) {
	close(p.accountStarted)
	defer close(p.accountStopped)
	select {
	case <-p.marketStarted:
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	if p.failMarket {
		<-ctx.Done()
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	return p.sdkPeer.GetAccount(ctx, request)
}

func (p *snapshotQueryPeer) ListOrders(ctx context.Context, request *pb.ListOrdersRequest) (*pb.ListOrdersResponse, error) {
	select {
	case <-p.accountStopped:
	default:
		return nil, status.Error(codes.Internal, "opening obligations read before account completed")
	}
	response, err := p.sdkPeer.ListOrders(ctx, request)
	p.ordersRead.Store(true)
	return response, err
}

func (p *snapshotQueryPeer) ListPositions(ctx context.Context, request *pb.ListPositionsRequest) (*pb.ListPositionsResponse, error) {
	if !p.ordersRead.Load() {
		return nil, status.Error(codes.Internal, "positions read before opening obligations completed")
	}
	p.positionsRead.Store(true)
	return p.sdkPeer.ListPositions(ctx, request)
}

func TestSnapshotOverlapsIndependentReadsAndKeepsFundObservationOrder(t *testing.T) {
	t.Parallel()
	peer := &snapshotQueryPeer{
		marketStarted:  make(chan struct{}),
		accountStarted: make(chan struct{}),
		accountStopped: make(chan struct{}),
	}
	client := sdkClient(t, peer, nil)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	snapshot, err := client.readSDK(ctx, Scope{
		UserID: 10001,
		FundID: 10001,
		Symbol: "BTCUSDT",
	})
	if err != nil || snapshot == nil || !peer.ordersRead.Load() || !peer.positionsRead.Load() {
		t.Fatalf("independent reads did not overlap or fund observation order changed: %v", err)
	}
	if snapshot.ReadAt.Before(snapshot.StartedAt) || snapshot.StartedAt.IsZero() {
		t.Fatal("concurrent observation lost its original freshness interval")
	}
}

func TestSnapshotReadFailureCancelsAndJoinsOtherReadBeforeReturning(t *testing.T) {
	t.Parallel()
	peer := &snapshotQueryPeer{
		marketStarted:  make(chan struct{}),
		accountStarted: make(chan struct{}),
		accountStopped: make(chan struct{}),
		failMarket:     true,
	}
	client := sdkClient(t, peer, nil)
	if _, err := client.readSDK(t.Context(), Scope{
		UserID: 10001,
		FundID: 10001,
		Symbol: "BTCUSDT",
	}); status.Code(err) != codes.Unavailable {
		t.Fatalf("market failure lost: %v", err)
	}
	select {
	case <-peer.accountStopped:
	case <-time.After(time.Second):
		t.Fatal("failed observation left its sibling running")
	}
	if peer.ordersRead.Load() || peer.positionsRead.Load() {
		t.Fatal("failed observation continued reading fund state")
	}
}
