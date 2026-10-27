package deer

import (
	"crypto/tls"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	probev1 "github.com/hmlax/muskdeer/api/probe/v1"
	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/testcert"
)

type refusedRequestCodec struct {
	refused <-chan struct{}
}

func TestRPCFactoryDoesNotReplayRefusedHTTP2Stream(t *testing.T) {
	t.Parallel()
	ca := testcert.New(t)
	address, attempts, refused := refusingHTTP2Server(t, ca)
	discovery := &rpcDiscoveryFixture{instances: []nacos.Instance{
		{
			ID:      "execution-a",
			Service: "execution.grpc",
			Address: address,
		},
	}}
	factory := newRPCFactory(t, ca, discovery, time.Second)
	connection, err := factory.Client(rpcclient.Target{Service: "execution.grpc"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = probev1.NewArithmeticProbeClient(connection).Multiply(t.Context(), &probev1.MultiplyRequest{
		Price:    "0.1",
		Quantity: "0.2",
	}, grpc.ForceCodec(refusedRequestCodec{refused: refused}))
	if status.Code(err) != codes.Unavailable || attempts.Load() != 1 {
		t.Fatalf("REFUSED_STREAM replayed: status=%v, attempts=%d", err, attempts.Load())
	}
}

func (c refusedRequestCodec) Marshal(value any) ([]byte, error) {
	// The server's PING ACK proves the client processed its earlier RST_STREAM
	// before SendMsg tries to write a business body. No sleep/race assumption.
	select {
	case <-c.refused:
	case <-time.After(time.Second):
		return nil, errors.New("fixture did not observe the RST_STREAM acknowledgement barrier")
	}
	message, ok := value.(proto.Message)
	if !ok {
		return nil, errors.New("fixture expects a protobuf message")
	}
	return proto.Marshal(message)
}

func (refusedRequestCodec) Unmarshal(data []byte, value any) error {
	message, ok := value.(proto.Message)
	if !ok {
		return errors.New("fixture expects a protobuf message")
	}
	return proto.Unmarshal(data, message)
}

func (refusedRequestCodec) Name() string { return "proto" }

func refusingHTTP2Server(t *testing.T, ca *testcert.Authority) (string, *atomic.Int32, <-chan struct{}) {
	t.Helper()
	security, err := rpcsecurity.Server(&configv1.GRPCServerSecurity{
		Mtls:                ca.Issue(t, "execution.grpc", false),
		AllowedPeerServices: []string{"api.grpc"},
	}, "execution.grpc")
	if err != nil {
		t.Fatal(err)
	}
	security.NextProtos = []string{"h2"}
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener = tls.NewListener(listener, security)
	var (
		mu     sync.Mutex
		client net.Conn
		closed bool
	)
	done := make(chan struct{})
	refused := make(chan struct{})
	attempts := &atomic.Int32{}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Error(err)
		}
		mu.Lock()
		closed = true
		if client != nil {
			if err := client.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				t.Error(err)
			}
		}
		mu.Unlock()
		<-done
	})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				t.Error(err)
			}
			return
		}
		defer func() {
			if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				t.Error(err)
			}
		}()
		mu.Lock()
		client = conn
		stopping := closed
		mu.Unlock()
		if stopping {
			return
		}
		if err := refuseHTTP2Requests(conn, attempts, refused); err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
			t.Error(err)
		}
	}()
	return listener.Addr().String(), attempts, refused
}

func refuseHTTP2Requests(conn net.Conn, attempts *atomic.Int32, refused chan struct{}) error {
	preface := make([]byte, len(http2.ClientPreface))
	if _, err := io.ReadFull(conn, preface); err != nil {
		return err
	}
	if string(preface) != http2.ClientPreface {
		return errors.New("fixture expected HTTP/2 preface")
	}
	framer := http2.NewFramer(conn, conn)
	if err := framer.WriteSettings(); err != nil {
		return err
	}
	var barrier sync.Once
	for {
		frame, err := framer.ReadFrame()
		if err != nil {
			return err
		}
		switch frame := frame.(type) {
		case *http2.SettingsFrame:
			if !frame.IsAck() {
				if err := framer.WriteSettingsAck(); err != nil {
					return err
				}
			}
		case *http2.HeadersFrame:
			attempts.Add(1)
			if err := framer.WriteRSTStream(frame.StreamID, http2.ErrCodeRefusedStream); err != nil {
				return err
			}
			if err := framer.WritePing(false, [8]byte{1}); err != nil {
				return err
			}
		case *http2.PingFrame:
			if frame.IsAck() && frame.Data == [8]byte{1} {
				barrier.Do(func() { close(refused) })
			}
		}
	}
}
