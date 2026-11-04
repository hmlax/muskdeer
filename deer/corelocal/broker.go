package corelocal

import (
	"context"
	"errors"
	"os"
	"slices"
	"time"

	"github.com/nats-io/nats.go"
	natsjs "github.com/nats-io/nats.go/jetstream"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	outbox "github.com/hmlax/muskdeer/deer/trading/outboxcontract"
)

// PrepareBroker creates bounded resources only in the caller's dedicated account.
// It refuses incompatible existing resources and never updates or deletes them.
func PrepareBroker(ctx context.Context, cfg *configv1.JetStream, consumer string) (result error) {
	if ctx == nil || cfg == nil || cfg.Username == "" || cfg.PasswordEnv == "" || os.Getenv(cfg.PasswordEnv) == "" || cfg.Stream == "" || cfg.SubjectPrefix == "" || consumer == "" {
		return ErrResources
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	conn, err := nats.Connect("nats://"+cfg.Address, nats.UserInfo(cfg.Username, os.Getenv(cfg.PasswordEnv)),
		nats.NoReconnect(), nats.Timeout(3*time.Second))
	if err != nil {
		return ErrResources
	}
	defer conn.Close()
	js, err := natsjs.New(conn, natsjs.WithDefaultTimeout(3*time.Second))
	if err != nil {
		return err
	}
	stream, err := js.Stream(ctx, cfg.Stream)
	if errors.Is(err, natsjs.ErrStreamNotFound) {
		stream, err = js.CreateStream(ctx, natsjs.StreamConfig{
			Name:       cfg.Stream,
			Subjects:   []string{cfg.SubjectPrefix + ".>"},
			Storage:    natsjs.FileStorage,
			Retention:  natsjs.LimitsPolicy,
			Discard:    natsjs.DiscardNew,
			MaxBytes:   16 * 1024 * 1024,
			MaxMsgs:    10000,
			MaxMsgSize: 64 * 1024,
			MaxAge:     24 * time.Hour,
			Duplicates: time.Minute,
		})
	}
	if err != nil {
		return err
	}
	if !matchesStream(stream.CachedInfo().Config, cfg.SubjectPrefix) {
		return ErrResources
	}
	bound, err := stream.Consumer(ctx, consumer)
	if errors.Is(err, natsjs.ErrConsumerNotFound) {
		bound, err = stream.CreateConsumer(ctx, natsjs.ConsumerConfig{
			Durable:       consumer,
			FilterSubject: cfg.SubjectPrefix + "." + outbox.ExecutionSubject,
			AckPolicy:     natsjs.AckExplicitPolicy,
			AckWait:       10 * time.Second,
			MaxDeliver:    5,
			MaxAckPending: 32,
			MaxWaiting:    8,
		})
	}
	if err != nil {
		return err
	}
	contract := bound.CachedInfo().Config
	if contract.Durable != consumer || contract.FilterSubject != cfg.SubjectPrefix+"."+outbox.ExecutionSubject ||
		len(contract.FilterSubjects) != 0 || contract.AckPolicy != natsjs.AckExplicitPolicy || contract.AckWait != 10*time.Second ||
		contract.MaxDeliver != 5 || contract.MaxAckPending != 32 || contract.MaxWaiting != 8 || contract.DeliverSubject != "" ||
		contract.DeliverPolicy != natsjs.DeliverAllPolicy || len(contract.BackOff) != 0 || contract.InactiveThreshold != 0 {
		return ErrResources
	}
	return nil
}

func matchesStream(stored natsjs.StreamConfig, prefix string) bool {
	return slices.Equal(stored.Subjects, []string{prefix + ".>"}) && stored.Storage == natsjs.FileStorage &&
		stored.MaxBytes == 16*1024*1024 && stored.Retention == natsjs.LimitsPolicy && stored.Discard == natsjs.DiscardNew &&
		stored.MaxMsgs == 10000 && stored.MaxMsgSize == 64*1024 && stored.MaxAge == 24*time.Hour && stored.Duplicates == time.Minute
}
