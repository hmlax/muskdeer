package jetstream

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"go.uber.org/goleak"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	probev1 "github.com/hmlax/muskdeer/api/probe/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestCanceledConstructorDoesNotConnect(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	client, err := New(ctx, testConfig(), "canceled")
	if client != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled New = %v, %v", client, err)
	}
}

func TestInvalidConfigurationBeforeIO(t *testing.T) {
	tests := []struct {
		name   string
		change func(*configv1.JetStream)
	}{
		{
			name:   "unbounded inflight",
			change: func(cfg *configv1.JetStream) { cfg.MaxInflight = 0 },
		},
		{
			name:   "unbounded payload",
			change: func(cfg *configv1.JetStream) { cfg.MaxMessageBytes = 0 },
		},
		{
			name:   "wildcard namespace",
			change: func(cfg *configv1.JetStream) { cfg.SubjectPrefix = "muskdeer.test.>" },
		},
		{
			name:   "missing stream",
			change: func(cfg *configv1.JetStream) { cfg.Stream = "" },
		},
		{
			name:   "missing deadline",
			change: func(cfg *configv1.JetStream) { cfg.CommandTimeout = nil },
		},
		{
			name:   "embedded credentials",
			change: func(cfg *configv1.JetStream) { cfg.Address = "nats://user:secret@127.0.0.1:4222" },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := testConfig()
			test.change(cfg)
			if err := validate(cfg); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestCancellationClosesSilentHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		cancel() // Cancel after TCP acceptance, while the server withholds INFO.
		if err != nil {
			done <- err
			return
		}
		<-stop
		done <- conn.Close()
	}()
	cfg := testConfig()
	cfg.Address = listener.Addr().String()
	started := time.Now()
	client, err := New(ctx, cfg, "silent-handshake")
	close(stop)
	if closeErr := listener.Close(); closeErr != nil {
		t.Error(closeErr)
	}
	if closeErr := <-done; closeErr != nil {
		t.Error(closeErr)
	}
	if client != nil {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
		t.Fatal("silent handshake returned a client")
	}
	if !errors.Is(err, context.Canceled) || time.Since(started) > 300*time.Millisecond {
		t.Fatalf("silent handshake cancellation = %v after %s", err, time.Since(started))
	}
}

func TestAdmissionWaitHonorsCommandDeadline(t *testing.T) {
	client := &Client{
		prefix:   "muskdeer.test.execution",
		timeout:  20 * time.Millisecond,
		maxBytes: 1024,
		inflight: make(chan struct{}, 1),
		closing:  make(chan struct{}),
	}
	client.inflight <- struct{}{}
	_, err := client.Publish(t.Context(), "probe.v1", "event-admission", &probev1.MultiplyRequest{
		Price:    "0.1",
		Quantity: "0.2",
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("exhausted publish admission = %v; want deadline", err)
	}
}

func TestSubjectsRequireResourceAndVersion(t *testing.T) {
	for _, suffix := range []string{"", "v1", "order.*", "order.v0", "order.v1.extra", "order.>.v1", "order.v1\n"} {
		if validSuffix(suffix) {
			t.Fatalf("invalid subject suffix %q accepted", suffix)
		}
	}
	if !validSuffix("order.state.v1") {
		t.Fatal("versioned resource rejected")
	}
}

func testConfig() *configv1.JetStream {
	return &configv1.JetStream{
		Address:         "127.0.0.1:4222",
		Stream:          "MUSKDEER_TEST",
		SubjectPrefix:   "muskdeer.test.execution",
		ConnectTimeout:  durationpb.New(time.Second),
		CommandTimeout:  durationpb.New(time.Second),
		MaxInflight:     2,
		MaxMessageBytes: 1024,
		TlsEnabled:      proto.Bool(false),
	}
}
