package jetstream

import (
	"testing"
	"time"

	natsjs "github.com/nats-io/nats.go/jetstream"
)

func TestConsumerRequiresExactScopeAndBoundedExplicitDelivery(t *testing.T) {
	fixture := natsjs.ConsumerInfo{
		Stream: "MUSKDEER_TEST",
		Name:   "portfolio_v1",
		Config: natsjs.ConsumerConfig{
			Durable:       "portfolio_v1",
			FilterSubject: "muskdeer.test.execution.execution.order.v1",
			AckPolicy:     natsjs.AckExplicitPolicy,
			DeliverPolicy: natsjs.DeliverAllPolicy,
			ReplayPolicy:  natsjs.ReplayInstantPolicy,
			AckWait:       30 * time.Second,
			MaxDeliver:    5,
			MaxAckPending: 64,
			MaxWaiting:    16,
		},
	}
	validate := func(info *natsjs.ConsumerInfo) error {
		return validateConsumer(info, "MUSKDEER_TEST", "portfolio_v1", fixture.Config.FilterSubject)
	}
	if err := validate(&fixture); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*natsjs.ConsumerInfo){
		func(i *natsjs.ConsumerInfo) { i.Stream = "OTHER" },
		func(i *natsjs.ConsumerInfo) { i.Name = "other" },
		func(i *natsjs.ConsumerInfo) { i.Config.Durable = "" },
		func(i *natsjs.ConsumerInfo) { i.Config.FilterSubject = "muskdeer.test.>" },
		func(i *natsjs.ConsumerInfo) { i.Config.FilterSubjects = []string{fixture.Config.FilterSubject} },
		func(i *natsjs.ConsumerInfo) { i.Config.AckPolicy = natsjs.AckAllPolicy },
		func(i *natsjs.ConsumerInfo) { i.Config.DeliverPolicy = natsjs.DeliverNewPolicy },
		func(i *natsjs.ConsumerInfo) { i.Config.MaxDeliver = -1 },
		func(i *natsjs.ConsumerInfo) { i.Config.MaxAckPending = 0 },
		func(i *natsjs.ConsumerInfo) { i.Config.MaxWaiting = 0 },
		func(i *natsjs.ConsumerInfo) { i.Config.AckWait = time.Millisecond },
	} {
		changed := fixture
		mutate(&changed)
		if err := validate(&changed); err == nil {
			t.Fatalf("unsafe durable consumer accepted: %+v", changed.Config)
		}
	}
}
