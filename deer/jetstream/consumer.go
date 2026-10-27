package jetstream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/nats-io/nats.go"
	natsjs "github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
)

// Consumer errors expose no raw payload, connection URL or broker response.
var (
	ErrInvalidConsumer = errors.New("jetstream consumer scope or delivery bounds are invalid")
	ErrInvalidDelivery = errors.New("jetstream delivery metadata or protobuf is invalid")
)

// DeliveryInfo identifies stored input independently of processing effects.
// PayloadSHA256 supports diagnosis without retaining or logging a raw payload.
type DeliveryInfo struct {
	EventID        string
	Stream         string
	Consumer       string
	StreamSequence uint64
	Attempt        uint64
	PayloadSHA256  string
}

// DeliveryError retains stable broker identity and the inspectable failure.
// Its text never includes the handler's raw error or message content.
type DeliveryError struct {
	Info  DeliveryInfo
	cause error
}

func (*DeliveryError) Error() string   { return "jetstream delivery was not acknowledged" }
func (e *DeliveryError) Unwrap() error { return e.cause }

// Consumer borrows the client's connection and owns no goroutine. One call per
// handle may process at a time; separate processes can share the same durable.
type Consumer struct {
	client   *Client
	consumer natsjs.Consumer
	name     string
	subject  string
	ackWait  time.Duration
	active   chan struct{}
}

// OpenConsumer binds an existing durable with an exact subject filter. It
// neither creates nor updates stream/consumer configuration.
func (c *Client) OpenConsumer(ctx context.Context, name, suffix string) (*Consumer, error) {
	if !validLabel(name) || !validSuffix(suffix) {
		return nil, ErrInvalidConsumer
	}
	bounded, cancel, err := c.commandContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	consumer, err := c.js.Consumer(bounded, c.stream, name)
	if err != nil {
		return nil, failed(bounded, "bind consumer", err)
	}
	info := consumer.CachedInfo()
	subject := c.prefix + "." + suffix
	if err := validateConsumer(info, c.stream, name, subject); err != nil {
		return nil, err
	}
	return &Consumer{
		client:   c,
		consumer: consumer,
		name:     name,
		subject:  subject,
		ackWait:  info.Config.AckWait,
		active:   make(chan struct{}, 1),
	}, nil
}

// ConsumeOne decodes at most one message into a borrowed typed destination.
// The callback must honor context and commit all effects before returning nil.
// Only then is a confirmed ACK attempted. An error never ACKs, NAKs, discards
// or retries inside this call; broker AckWait/MaxDeliver governs redelivery.
// Empty pulls return false,nil. Caller cancellation remains an error.
func (c *Consumer) ConsumeOne(ctx context.Context, destination proto.Message, handle func(context.Context, DeliveryInfo) error) (bool, error) {
	if ctx == nil || destination == nil || !destination.ProtoReflect().IsValid() || handle == nil {
		return false, ErrInvalidDelivery
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return false, ErrInvalidDelivery
	}
	work, cancel := context.WithTimeout(ctx, c.ackWait/2)
	defer cancel()
	select {
	case c.active <- struct{}{}:
		defer func() { <-c.active }()
	case <-work.Done():
		return false, work.Err()
	case <-c.client.closing:
		return false, ErrClosed
	}
	if err := c.Check(work); err != nil {
		return false, err
	}
	message, err := c.next(work)
	if err != nil || message == nil {
		return false, err
	}
	info, err := c.decode(message, destination)
	if err == nil {
		err = work.Err()
	}
	if err == nil {
		err = handle(work, info)
	}
	if err == nil {
		err = c.ack(work, message)
	}
	if err != nil {
		return true, &DeliveryError{
			Info:  info,
			cause: errors.Join(err, work.Err()),
		}
	}
	return true, nil
}

// Check verifies the current durable still has the required scope and delivery
// bounds. A changed acknowledgement window requires constructing a new handle.
func (c *Consumer) Check(ctx context.Context) error {
	bounded, cancel, err := c.client.commandContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	info, err := c.consumer.Info(bounded)
	if err != nil {
		return failed(bounded, "check consumer", err)
	}
	if err := validateConsumer(info, c.client.stream, c.name, c.subject); err != nil {
		return err
	}
	if info.Config.AckWait != c.ackWait {
		return ErrInvalidConsumer
	}
	return bounded.Err()
}

func (c *Consumer) next(ctx context.Context) (natsjs.Msg, error) {
	bounded, cancel, err := c.client.commandContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	message, err := c.consumer.Next(natsjs.FetchContext(bounded))
	if bounded.Err() != nil {
		return nil, failed(bounded, "fetch", errors.Join(err, bounded.Err()))
	}
	if errors.Is(err, nats.ErrTimeout) {
		return nil, nil
	}
	return message, failed(bounded, "fetch", err)
}

func (c *Consumer) ack(ctx context.Context, message natsjs.Msg) error {
	bounded, cancel, err := c.client.commandContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	return failed(bounded, "confirm acknowledgement", message.DoubleAck(bounded))
}

func (c *Consumer) decode(message natsjs.Msg, destination proto.Message) (DeliveryInfo, error) {
	digest := sha256.Sum256(message.Data())
	info := DeliveryInfo{
		Stream:        c.client.stream,
		Consumer:      c.name,
		PayloadSHA256: hex.EncodeToString(digest[:]),
	}
	metadata, err := message.Metadata()
	if err != nil || metadata.Stream != info.Stream || metadata.Consumer != info.Consumer || metadata.Sequence.Stream == 0 || metadata.NumDelivered == 0 {
		return info, ErrInvalidDelivery
	}
	info.StreamSequence = metadata.Sequence.Stream
	info.Attempt = metadata.NumDelivered
	ids := message.Headers().Values("Nats-Msg-Id")
	if len(ids) != 1 || !validID(ids[0]) || message.Subject() != c.subject ||
		len(message.Headers().Values("Content-Type")) != 1 || message.Headers().Get("Content-Type") != "application/protobuf" || len(message.Data()) > c.client.maxBytes {
		return info, ErrInvalidDelivery
	}
	info.EventID = ids[0]
	if err := (proto.UnmarshalOptions{RecursionLimit: 64}).Unmarshal(message.Data(), destination); err != nil {
		return info, errors.Join(ErrInvalidDelivery, err)
	}
	return info, nil
}

func validateConsumer(info *natsjs.ConsumerInfo, stream, name, subject string) error {
	if info == nil || info.Stream != stream || info.Name != name {
		return ErrInvalidConsumer
	}
	config := info.Config
	if config.Durable != name || config.FilterSubject != subject || len(config.FilterSubjects) != 0 ||
		config.AckPolicy != natsjs.AckExplicitPolicy || config.DeliverPolicy != natsjs.DeliverAllPolicy || config.ReplayPolicy != natsjs.ReplayInstantPolicy ||
		config.AckWait < 2*time.Second || config.AckWait > 5*time.Minute || len(config.BackOff) != 0 || config.InactiveThreshold != 0 ||
		config.MaxDeliver < 1 || config.MaxDeliver > 100 || config.MaxAckPending < 1 || config.MaxAckPending > 1024 || config.MaxWaiting < 1 || config.MaxWaiting > 64 {
		return ErrInvalidConsumer
	}
	return nil
}
