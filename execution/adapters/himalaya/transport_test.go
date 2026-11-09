package himalaya

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	"github.com/hmlax/muskdeer/deer/trading/domain"
	gateway "github.com/hmlax/muskdeer/execution/venue/gateway"
)

func TestSDKCannotReplayARefusedHTTP2Stream(t *testing.T) {
	testSDKRefusedStream(t, false)
}

func TestSDKCannotReplaySyntheticTradeOnARefusedHTTP2Stream(t *testing.T) {
	testSDKRefusedStream(t, true)
}

func testSDKRefusedStream(t *testing.T, synthetic bool) {
	t.Helper()
	t.Parallel()
	listener := bufconn.Listen(1 << 20)
	refused := make(chan struct{})
	attempts := &atomic.Int64{}
	done := make(chan struct{})
	var mu sync.Mutex
	var connection net.Conn
	stopping := false
	t.Cleanup(func() {
		_ = listener.Close()
		mu.Lock()
		stopping = true
		if connection != nil {
			_ = connection.Close()
		}
		mu.Unlock()
		<-done
	})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		mu.Lock()
		connection = conn
		stop := stopping
		mu.Unlock()
		if stop {
			return
		}
		if err := sdkRefusingFrames(conn, attempts, refused); err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.ErrClosedPipe) {
			t.Error(err)
		}
	}()
	client, err := NewClient(t.Context(), Config{
		Target:  "passthrough:///sdk-refused-stream",
		Token:   "sdk-test-token",
		Timeout: time.Second,
		DialOptions: []grpc.DialOption{
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
			grpc.WithDefaultCallOptions(grpc.ForceCodec(sdkRefusedCodec{refused: refused})),
		},
	}, &testAdmission{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(context.WithoutCancel(t.Context())); err != nil {
			t.Error(err)
		}
	})
	if synthetic {
		if _, err := client.WashTrade(t.Context(), &gateway.SyntheticRequest{
			Symbol:   "BTCUSDT",
			Side:     domain.SideBuy,
			Quantity: "0.001",
			Price:    "100.1",
		}); err == nil || attempts.Load() != 1 {
			t.Fatalf("refused synthetic SDK request was replayed: attempts=%d err=%v", attempts.Load(), err)
		}
		return
	}
	if _, err := client.Submit(t.Context(), sdkOrder()); !errors.Is(err, domain.ErrOutcomeUnknown) || attempts.Load() != 1 {
		t.Fatalf("refused SDK request was replayed: attempts=%d err=%v", attempts.Load(), err)
	}
}

type sdkRefusedCodec struct{ refused <-chan struct{} }

func (c sdkRefusedCodec) Marshal(value any) ([]byte, error) {
	select {
	case <-c.refused:
	case <-time.After(time.Second):
		return nil, errors.New("fixture did not observe the HTTP/2 refusal barrier")
	}
	message, ok := value.(proto.Message)
	if !ok {
		return nil, errors.New("fixture requires a protobuf message")
	}
	return proto.Marshal(message)
}

func (sdkRefusedCodec) Unmarshal(data []byte, value any) error {
	message, ok := value.(proto.Message)
	if !ok {
		return errors.New("fixture requires a protobuf message")
	}
	return proto.Unmarshal(data, message)
}

func (sdkRefusedCodec) Name() string { return "proto" }

func sdkRefusingFrames(conn net.Conn, attempts *atomic.Int64, refused chan struct{}) error {
	preface := make([]byte, len(http2.ClientPreface))
	if _, err := io.ReadFull(conn, preface); err != nil {
		return err
	}
	if string(preface) != http2.ClientPreface {
		return errors.New("fixture expected the HTTP/2 client preface")
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
