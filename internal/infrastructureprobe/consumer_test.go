//go:build integration

package infrastructureprobe

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	natsjs "github.com/nats-io/nats.go/jetstream"

	probev1 "github.com/hmlax/muskdeer/api/probe/v1"
	"github.com/hmlax/muskdeer/deer/jetstream"
)

func TestConsumerBindingDoesNotProvisionMissingDurable(t *testing.T) {
	client, _, _ := consumerFixture(t, "binding")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if bound, err := client.OpenConsumer(ctx, "absent", "probe.v1"); bound != nil || !errors.Is(err, natsjs.ErrConsumerNotFound) {
		t.Fatalf("missing durable was not rejected: %v, %v", bound, err)
	}
	if _, err := jetStreamAdmin(t).Consumer(ctx, "MUSKDEER_CONSUMER_binding", "absent"); !errors.Is(err, natsjs.ErrConsumerNotFound) {
		t.Fatalf("binding created a consumer: %v", err)
	}
}

func TestConsumerEmptyPullKeepsIdleWorkerAliveWithoutHandlerOrAck(t *testing.T) {
	_, consumer, raw := consumerFixture(t, "empty_pull")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	calls := 0
	for range 3 {
		call, stop := context.WithTimeout(ctx, 3*time.Second)
		worked, err := consumer.ConsumeOne(call, &probev1.MultiplyReply{}, func(context.Context, jetstream.DeliveryInfo) error {
			calls++
			return nil
		})
		contextErr := call.Err()
		stop()
		if worked || err != nil || contextErr != nil || calls != 0 {
			t.Fatalf("empty pull became worker failure or delivery: worked=%t error=%v context=%v calls=%d", worked, err, contextErr, calls)
		}
	}
	info, err := raw.Info(ctx)
	if err != nil || info.NumAckPending != 0 || info.AckFloor.Stream != 0 || info.Delivered.Stream != 0 {
		t.Fatal("empty pull created or acknowledged a delivery", err)
	}
}

func TestConsumerRejectsMalformedProtobufWithoutAckOrHandler(t *testing.T) {
	_, consumer, raw := consumerFixture(t, "malformed")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_, err := jetStreamAdmin(t).PublishMsg(ctx, &nats.Msg{
		Subject: "muskdeer.integration.malformed.probe.v1",
		Header: nats.Header{
			"Content-Type": []string{"application/protobuf"},
			"Nats-Msg-Id":  []string{"malformed:1"},
		},
		Data: []byte{0xff},
	})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	worked, err := consumer.ConsumeOne(ctx, &probev1.MultiplyReply{}, func(context.Context, jetstream.DeliveryInfo) error {
		calls++
		return nil
	})
	var delivery *jetstream.DeliveryError
	if !worked || !errors.Is(err, jetstream.ErrInvalidDelivery) || !errors.As(err, &delivery) || delivery.Info.StreamSequence != 1 || len(delivery.Info.PayloadSHA256) != 64 || calls != 0 {
		t.Fatalf("invalid input reached handler or lost diagnosis: %v, %v, calls=%d", worked, err, calls)
	}
	info, err := raw.Info(ctx)
	if err != nil || info.NumAckPending != 1 {
		t.Fatalf("malformed input was acknowledged: %+v, %v", info, err)
	}
}

func TestConsumerRejectsChangedScopeBeforeFetching(t *testing.T) {
	_, consumer, raw := consumerFixture(t, "changed_scope")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	info, err := raw.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	config := info.Config
	config.FilterSubject = "muskdeer.integration.changed_scope.other.v1"
	if _, err := jetStreamAdmin(t).UpdateConsumer(ctx, info.Stream, config); err != nil {
		t.Fatal(err)
	}
	called := false
	worked, err := consumer.ConsumeOne(ctx, &probev1.MultiplyReply{}, func(context.Context, jetstream.DeliveryInfo) error {
		called = true
		return nil
	})
	if worked || !errors.Is(err, jetstream.ErrInvalidConsumer) || called {
		t.Fatalf("changed consumer scope reached business effects: %v, %v", worked, err)
	}
}

func TestConsumerCancellationBoundsHandlerAndWaitingAdmission(t *testing.T) {
	client, consumer, raw := consumerFixture(t, "cancellation")
	if _, err := client.Publish(t.Context(), "probe.v1", "consumer:1", &probev1.MultiplyReply{Notional: "0.02"}); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	finished := make(chan error, 1)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	go func() {
		_, err := consumer.ConsumeOne(ctx, &probev1.MultiplyReply{}, func(work context.Context, _ jetstream.DeliveryInfo) error {
			close(entered)
			<-work.Done()
			return work.Err()
		})
		finished <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		err := <-finished
		t.Fatalf("consumer never entered handler: %v", err)
	}
	waiting, cancelWaiting := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancelWaiting()
	called := false
	worked, err := consumer.ConsumeOne(waiting, &probev1.MultiplyReply{}, func(context.Context, jetstream.DeliveryInfo) error {
		called = true
		return nil
	})
	cancel()
	firstErr := <-finished
	if worked || !errors.Is(err, context.DeadlineExceeded) || called || !errors.Is(firstErr, context.Canceled) {
		t.Fatalf("waiting/callback cancellation was lost: %v, %v, %v", worked, err, firstErr)
	}
	proof, cancelProof := context.WithTimeout(t.Context(), time.Second)
	defer cancelProof()
	info, err := raw.Info(proof)
	if err != nil || info.NumAckPending != 1 {
		t.Fatalf("canceled handler was acknowledged: %+v, %v", info, err)
	}
}

func consumerFixture(t *testing.T, name string) (*jetstream.Client, *jetstream.Consumer, natsjs.Consumer) {
	t.Helper()
	cfg := jetStreamConfig()
	cfg.Stream = "MUSKDEER_CONSUMER_" + name
	cfg.SubjectPrefix = "muskdeer.integration." + name
	stream := provisionStream(t, cfg)
	raw, err := stream.CreateConsumer(t.Context(), natsjs.ConsumerConfig{
		Durable:       "deer_consumer_test",
		FilterSubject: cfg.SubjectPrefix + ".probe.v1",
		AckPolicy:     natsjs.AckExplicitPolicy,
		AckWait:       4 * time.Second,
		MaxDeliver:    2,
		MaxAckPending: 1,
		MaxWaiting:    4,
	})
	if err != nil {
		t.Fatal(err)
	}
	client := openPublisher(t, cfg)
	consumer, err := client.OpenConsumer(t.Context(), "deer_consumer_test", "probe.v1")
	if err != nil {
		t.Fatal(err)
	}
	return client, consumer, raw
}
