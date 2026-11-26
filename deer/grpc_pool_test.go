package deer

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/peer"

	"github.com/hmlax/muskdeer/deer/nacos"
	probev1 "github.com/hmlax/muskdeer/deer/pb/common/probe/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/testcert"
)

type rpcReuseProbe struct {
	probev1.UnimplementedArithmeticProbeServer
	mu    sync.Mutex
	peers map[string]struct{}
}

func (p *rpcReuseProbe) Multiply(ctx context.Context, _ *probev1.MultiplyRequest) (*probev1.MultiplyReply, error) {
	remote, _ := peer.FromContext(ctx)
	p.mu.Lock()
	p.peers[remote.Addr.String()] = struct{}{}
	p.mu.Unlock()
	return &probev1.MultiplyReply{Notional: "1"}, nil
}

func TestRPCFactoryReusesTransportAndDiscoveryAcrossCalls(t *testing.T) {
	t.Parallel()
	ca := testcert.New(t)
	probe := &rpcReuseProbe{peers: make(map[string]struct{})}
	node := runRPCFactoryNode(t, ca, probe)
	discovery := &rpcDiscoveryFixture{instances: []nacos.Instance{
		{
			ID:      "execution-a",
			Service: "execution.grpc",
			Address: node.GRPCAddress(),
		},
	}}
	factory := newRPCFactory(t, ca, discovery, time.Second)
	for range 8 {
		conn, err := factory.Client(rpcclient.Target{Service: "execution.grpc"})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		_, err = probev1.NewArithmeticProbeClient(conn).Multiply(ctx, &probev1.MultiplyRequest{})
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	}
	probe.mu.Lock()
	connections := len(probe.peers)
	probe.mu.Unlock()
	discovery.mu.Lock()
	lookups := discovery.lookups
	discovery.mu.Unlock()
	if connections != 1 || lookups >= 8 {
		t.Fatalf("8 healthy RPCs used connections=%d discoveries=%d, want one reused transport and shared discovery", connections, lookups)
	}
}
