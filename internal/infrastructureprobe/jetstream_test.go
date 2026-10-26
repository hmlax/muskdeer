//go:build integration

package infrastructureprobe

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/nats-io/nats.go"
	natsjs "github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	probev1 "github.com/hmlax/muskdeer/api/probe/v1"
	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/jetstream"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

func TestJetStreamInfrastructure(t *testing.T) {
	if os.Getenv("MUSKDEER_TEST_NATS_TOKEN") == "" {
		t.Fatal("isolated NATS token is required; run make integration")
	}
	provisionStream(t, jetStreamConfig())
	t.Run("typed payload storage and duplicate receipt", testStoredPublication)
	t.Run("invalid publication never reaches storage", testInvalidPublication)
	t.Run("stored publication without acknowledgement is ambiguous", testAmbiguousPublication)
	t.Run("missing stream does not auto provision", testMissingStream)
	t.Run("misrouted namespace fails initialization", testMisroutedStream)
	t.Run("deer readiness observes removed stream", testJetStreamReadiness)
	t.Run("API failure closes shared publisher", testJetStreamAPIRollback)
	t.Run("authentication cause hides token", testJetStreamAuthentication)
}

func testStoredPublication(t *testing.T) {
	const eventID = "probe-stored-001"
	cfg := jetStreamConfig()
	client := openPublisher(t, cfg)
	message := &probev1.MultiplyRequest{
		Price:    "12345678901234567890.123456789012345678",
		Quantity: "0.000000000000000001",
	}
	ack, err := client.Publish(t.Context(), "probe.v1", eventID, message)
	if err != nil || ack == nil || ack.Duplicate || ack.Sequence == 0 || ack.Stream != cfg.Stream {
		t.Fatalf("storage receipt = %+v, %v", ack, err)
	}
	duplicate, err := client.Publish(t.Context(), "probe.v1", eventID, message)
	if err != nil || duplicate == nil || !duplicate.Duplicate || duplicate.Sequence != ack.Sequence {
		t.Fatalf("duplicate receipt = %+v, %v", duplicate, err)
	}
	js := jetStreamAdmin(t)
	stream, err := js.Stream(t.Context(), cfg.Stream)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := stream.GetMsg(t.Context(), ack.Sequence)
	if err != nil {
		t.Fatal(err)
	}
	var decoded probev1.MultiplyRequest
	if err := proto.Unmarshal(stored.Data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(message, &decoded) || stored.Subject != cfg.SubjectPrefix+".probe.v1" || stored.Header.Get("Nats-Msg-Id") != eventID {
		t.Fatalf("typed storage round trip = %+v, %v", stored, &decoded)
	}
	for range 2 {
		if err := client.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.Publish(t.Context(), "probe.v1", "after-close", message); !errors.Is(err, jetstream.ErrClosed) {
		t.Fatalf("closed publisher = %v", err)
	}
}

func testInvalidPublication(t *testing.T) {
	cfg := jetStreamConfig()
	client := openPublisher(t, cfg)
	js := jetStreamAdmin(t)
	stream, err := js.Stream(t.Context(), cfg.Stream)
	if err != nil {
		t.Fatal(err)
	}
	before := stream.CachedInfo().State.Msgs
	message := &probev1.MultiplyReply{Notional: "0.02"}
	if _, err := client.Publish(t.Context(), "probe.*", "invalid-wildcard", message); err == nil {
		t.Fatal("wildcard publication accepted")
	}
	if _, err := client.Publish(t.Context(), "probe.v1", "", message); err == nil {
		t.Fatal("missing identity accepted")
	}
	if _, err := client.Publish(t.Context(), "probe.v1", "nil-message", nil); err == nil {
		t.Fatal("untyped payload accepted")
	}
	if _, err := client.Publish(t.Context(), "probe.v1", "oversized", &probev1.MultiplyReply{Notional: strings.Repeat("1", 2048)}); err == nil {
		t.Fatal("oversized payload accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.Publish(ctx, "probe.v1", "canceled", message); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled publish = %v", err)
	}
	info, err := stream.Info(t.Context())
	if err != nil || info.State.Msgs != before {
		t.Fatalf("invalid publication changed storage: %+v, %v", info, err)
	}
}

func testAmbiguousPublication(t *testing.T) {
	cfg := jetStreamConfig()
	cfg.Stream = "MUSKDEER_AMBIGUOUS"
	cfg.SubjectPrefix = "muskdeer.integration.ambiguous"
	stream := provisionStream(t, cfg)
	client := openPublisher(t, cfg)
	streamConfig := stream.CachedInfo().Config
	streamConfig.NoAck = true
	if _, err := jetStreamAdmin(t).UpdateStream(t.Context(), streamConfig); err != nil {
		t.Fatal(err)
	}
	if client, err := jetstream.New(t.Context(), cfg, "no-ack-rejection"); client != nil || err == nil {
		t.Fatalf("unacknowledged stream constructor = %v, %v", client, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	message := &probev1.MultiplyReply{Notional: "0.02"}
	ack, err := client.Publish(ctx, "probe.v1", "ambiguous-001", message)
	if ack != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("missing storage ACK = %+v, %v", ack, err)
	}
	stored, err := stream.GetMsg(t.Context(), 1)
	if err != nil {
		t.Fatalf("timed-out publication was not stored: %v", err)
	}
	var decoded probev1.MultiplyReply
	if err := proto.Unmarshal(stored.Data, &decoded); err != nil || !proto.Equal(message, &decoded) {
		t.Fatalf("ambiguous payload = %v, %v", &decoded, err)
	}
	info, err := stream.Info(t.Context())
	if err != nil || info.State.Msgs != 1 {
		t.Fatalf("ambiguous publication was replayed: %+v, %v", info, err)
	}
	streamConfig.NoAck = false
	if _, err := jetStreamAdmin(t).UpdateStream(t.Context(), streamConfig); err != nil {
		t.Fatal(err)
	}
	ack, err = client.Publish(t.Context(), "probe.v1", "ambiguous-001", message)
	if err != nil || ack == nil || !ack.Duplicate || ack.Sequence != 1 {
		t.Fatalf("owner retry with stable identity = %+v, %v", ack, err)
	}
}

func testMissingStream(t *testing.T) {
	cfg := jetStreamConfig()
	cfg.Stream = "MUSKDEER_MISSING"
	client, err := jetstream.New(t.Context(), cfg, "missing-stream")
	if client != nil || !errors.Is(err, natsjs.ErrStreamNotFound) {
		t.Fatalf("missing stream = %v, %v", client, err)
	}
	if _, err := jetStreamAdmin(t).Stream(t.Context(), cfg.Stream); !errors.Is(err, natsjs.ErrStreamNotFound) {
		t.Fatalf("application unexpectedly provisioned missing stream: %v", err)
	}
}

func testJetStreamReadiness(t *testing.T) {
	cfg := serviceConfig("jetstream-ready")
	cfg.Postgres = nil
	cfg.Jetstream = jetStreamConfig()
	cfg.Jetstream.Stream = "MUSKDEER_READY"
	cfg.Jetstream.SubjectPrefix = "muskdeer.integration.ready"
	provisionStream(t, cfg.Jetstream)
	service, cleanup, err := deer.NewDeer(t.Context(), cfg, deer.ServiceSpec{
		LogOutput: io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cleanup(t.Context()); err != nil {
			t.Error(err)
		}
	})
	if _, err := service.JetStream(); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- service.Run(t.Context()) }()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := service.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	if err := service.CheckReady(ctx); err != nil {
		t.Fatal(err)
	}
	if err := jetStreamAdmin(t).DeleteStream(ctx, cfg.Jetstream.Stream); err != nil {
		t.Fatal(err)
	}
	if err := service.CheckReady(ctx); !errors.Is(err, natsjs.ErrStreamNotFound) {
		t.Fatalf("removed stream readiness = %v", err)
	}
	if err := cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func testMisroutedStream(t *testing.T) {
	cfg := jetStreamConfig()
	cfg.SubjectPrefix = "muskdeer.integration.portfolio"
	client, err := jetstream.New(t.Context(), cfg, "misrouted-namespace")
	if client != nil || err == nil {
		t.Fatalf("stream without namespace coverage = %v, %v", client, err)
	}
}

func testJetStreamAPIRollback(t *testing.T) {
	cfg := serviceConfig("api-jetstream-rollback")
	cfg.Redis = redisConfig()
	cfg.Jetstream = jetStreamConfig()
	cfg.Http = &configv1.Listener{
		Address:        "127.0.0.1:0",
		RequestTimeout: durationpb.New(time.Second),
	}
	failure := errors.New("route assembly rejected")
	var publisher *jetstream.Client
	api, cleanup, err := deer.NewDeerApi(t.Context(), cfg, deer.APISpec{
		Service: deer.ServiceSpec{
			LogOutput: io.Discard,
		},
		Routes: func(_ context.Context, owner *deer.Deer, _ *server.Hertz) error {
			var err error
			publisher, err = owner.JetStream()
			if err != nil {
				return err
			}
			return failure
		},
	})
	if api != nil || cleanup != nil || !errors.Is(err, failure) {
		t.Fatalf("API initialization = %v, cleanup nil %v, %v", api, cleanup == nil, err)
	}
	if publisher == nil || !errors.Is(publisher.Ping(t.Context()), jetstream.ErrClosed) {
		t.Fatal("API failure did not close shared publisher")
	}
}

func testJetStreamAuthentication(t *testing.T) {
	const token = "invalid_nats_test_only"
	const reference = "MUSKDEER_TEST_INVALID_NATS_TOKEN"
	t.Setenv(reference, token)
	cfg := jetStreamConfig()
	cfg.TokenEnv = reference
	client, err := jetstream.New(t.Context(), cfg, "invalid-token")
	if client != nil || !errors.Is(err, nats.ErrAuthorization) {
		t.Fatalf("authentication rejection = %v, %v", client, err)
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("authentication error leaked token: %v", err)
	}
}

func openPublisher(t *testing.T, cfg *configv1.JetStream) *jetstream.Client {
	t.Helper()
	client, err := jetstream.New(t.Context(), cfg, "publisher-probe")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	return client
}

func provisionStream(t *testing.T, cfg *configv1.JetStream) natsjs.Stream {
	t.Helper()
	stream, err := jetStreamAdmin(t).CreateStream(t.Context(), natsjs.StreamConfig{
		Name:       cfg.Stream,
		Subjects:   []string{cfg.SubjectPrefix + ".>"},
		Storage:    natsjs.FileStorage,
		Replicas:   1,
		Retention:  natsjs.LimitsPolicy,
		Discard:    natsjs.DiscardNew,
		MaxBytes:   4 * 1024 * 1024,
		MaxMsgs:    1000,
		MaxMsgSize: 64 * 1024,
		MaxAge:     10 * time.Minute,
		Duplicates: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	return stream
}

func jetStreamAdmin(t *testing.T) natsjs.JetStream {
	t.Helper()
	conn, err := nats.Connect("nats://"+jetStreamConfig().Address,
		nats.Token(os.Getenv("MUSKDEER_TEST_NATS_TOKEN")), nats.NoReconnect(), nats.Timeout(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	js, err := natsjs.New(conn, natsjs.WithDefaultTimeout(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	return js
}

func jetStreamConfig() *configv1.JetStream {
	return &configv1.JetStream{
		Address:         "127.0.0.1:55441",
		Stream:          "MUSKDEER_TEST",
		SubjectPrefix:   "muskdeer.integration.execution",
		TokenEnv:        "MUSKDEER_TEST_NATS_TOKEN",
		ConnectTimeout:  durationpb.New(time.Second),
		CommandTimeout:  durationpb.New(time.Second),
		MaxInflight:     2,
		MaxMessageBytes: 1024,
		TlsEnabled:      proto.Bool(false),
	}
}
