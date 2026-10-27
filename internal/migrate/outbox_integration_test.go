//go:build integration

package migrate

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	natsjs "github.com/nats-io/nats.go/jetstream"
	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/jetstream"
	"github.com/hmlax/muskdeer/deer/ownerid"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/outbox"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/usecase"
)

func TestOutboxClaimsExcludePeersAndFenceExpiredOwner(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	intents, submit := admissionFixture(ctx, t, db)
	if _, err := intents.Accept(ctx, submit.Input, submit.ExecutionAuthority); err != nil {
		t.Fatal(err)
	}
	store := outboxStoreFixture(t, db)
	type result struct {
		record *outbox.Record
		err    error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for range 2 {
		owner := outboxOwner(t)
		go func() {
			<-start
			record, err := store.Claim(ctx, owner, time.Minute)
			results <- result{
				record: record,
				err:    err,
			}
		}()
	}
	close(start)
	var claimed []*outbox.Record
	for range 2 {
		got := <-results
		if got.err != nil {
			t.Error(got.err)
		}
		if got.record != nil {
			claimed = append(claimed, got.record)
		}
	}
	if t.Failed() || len(claimed) != 1 {
		t.Fatalf("expected exactly one claim, got %d", len(claimed))
	}
	old := claimed[0]
	if err := db.WithContext(ctx).Model(&modelv1.OutboxEvent{}).Where("id = ?", old.ID).UpdateColumn("claim_expires_at", 0).Error; err != nil {
		t.Fatal(err)
	}
	successor, err := store.Claim(ctx, outboxOwner(t), time.Minute)
	if err != nil || successor == nil || successor.ID != old.ID {
		t.Fatalf("expired claim recovery = %+v, %v", successor, err)
	}
	if err := store.Complete(ctx, old); !errors.Is(err, outbox.ErrClaimLost) {
		t.Fatalf("old owner completed successor claim: %v", err)
	}
	if err := store.Retry(ctx, successor, outbox.CodePublishFailed, time.Minute); err != nil {
		t.Fatal(err)
	}
	if premature, err := store.Claim(ctx, outboxOwner(t), time.Second); err != nil || premature != nil {
		t.Fatalf("persisted retry deadline ignored: %+v, %v", premature, err)
	}
	var row modelv1.OutboxEvent
	if err := db.WithContext(ctx).Model(&modelv1.OutboxEvent{}).First(&row, old.ID).Error; err != nil || row.Attempts != 2 || row.State != domain.OutboxStatePending || row.ErrorCode != outbox.CodePublishFailed || row.ClaimedBy != "" || row.ClaimExpiresAt != 0 {
		t.Fatalf("retry state = %v, %v", &row, err)
	}
}

func TestOutboxStoredBeforeCompletionAndInboxCommittedBeforeAck(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	portfolio := portfolioFixture(ctx, t, db, submit)
	portfolioTrades(ctx, t, core, intents, submit)
	stream, consumer, publisher := outboxBrokerFixture(ctx, t)
	store := outboxStoreFixture(t, db)
	injected := errors.New("controlled completion interruption after broker storage")
	fault := &interruptedOutboxStore{
		Store: store,
		cause: injected,
	}
	worker, err := outbox.New(fault, publisher, outboxOwner(t))
	if err != nil {
		t.Fatal(err)
	}
	for range 30 {
		worked, err := worker.RunOne(ctx)
		if errors.Is(err, injected) {
			break
		}
		if err != nil || !worked {
			t.Fatalf("publication stopped before controlled failure: %v, %v", worked, err)
		}
	}
	if fault.interrupted == nil {
		t.Fatal("did not reach the stored trade publication window")
	}
	before, err := stream.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.OutboxEvent{}).Where("id = ? AND state = ?", fault.interrupted.ID, domain.OutboxStatePublishing).
		UpdateColumn("claim_expires_at", 0).Error; err != nil {
		t.Fatal(err)
	}
	recovered, err := outbox.New(store, publisher, outboxOwner(t))
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := recovered.RunOne(ctx); err != nil || !worked {
		t.Fatalf("stored publication recovery = %v, %v", worked, err)
	}
	after, err := stream.Info(ctx)
	if err != nil || after.State.Msgs != before.State.Msgs {
		t.Fatalf("same event ID did not deduplicate storage: %v, %v", after, err)
	}
	for range 30 {
		worked, err := recovered.RunOne(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !worked {
			break
		}
	}
	var remaining int64
	if err := db.WithContext(ctx).Model(&modelv1.OutboxEvent{}).Where("state <> ?", domain.OutboxStatePublished).Count(&remaining).Error; err != nil || remaining != 0 {
		t.Fatalf("unpublished events = %d, %v", remaining, err)
	}
	info, err := stream.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := usecase.NewFillConsumer(portfolio, intentServiceFixture(t, intents, core))
	if err != nil {
		t.Fatal(err)
	}
	consumePortfolioWithRedelivery(ctx, t, consumer, handler, info.State.Msgs)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertPortfolioRowCount(ctx, t, db, &modelv1.PortfolioFill{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.ConsumerInbox{}, 2)
	projection, err := portfolio.Get(ctx, submit.Input.AccountID)
	if err != nil || !projection.Reconciled || !projection.Values.Quantity.Equal(decimal.NewFromInt(10)) || !projection.Values.CashQuote.Equal(decimal.RequireFromString("1001.8")) {
		t.Fatalf("publication/redelivery changed business accounting: %+v, %v", projection, err)
	}
}

func consumePortfolioWithRedelivery(ctx context.Context, t *testing.T, consumer natsjs.Consumer, handler *usecase.FillConsumer, count uint64) {
	t.Helper()
	redelivered := false
	trades := 0
	for range count + 1 {
		message, err := consumer.Next(natsjs.FetchContext(ctx))
		if err != nil {
			t.Fatal(err)
		}
		var event commonv1.ExecutionEvent
		if err := proto.Unmarshal(message.Data(), &event); err != nil {
			t.Fatal(err)
		}
		if event.EventType == domain.EventTradeFilled {
			trades++
			if err := handler.Apply(ctx, portfolioNotice(ctx, t, &event)); err != nil {
				t.Fatal(err)
			}
			if !redelivered {
				redelivered = true
				// Effects are committed but the delivery is deliberately not ACKed.
				if err := message.Nak(); err != nil {
					t.Fatal(err)
				}
				continue
			}
		}
		if err := message.DoubleAck(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if !redelivered || trades != 3 {
		t.Fatalf("expected two trades and one actual broker redelivery, got %d", trades)
	}
}

type interruptedOutboxStore struct {
	outbox.Store
	cause       error
	interrupted *outbox.Record
}

func (s *interruptedOutboxStore) Complete(ctx context.Context, record *outbox.Record) error {
	if s.interrupted == nil && record.Event.EventType == domain.EventTradeFilled {
		s.interrupted = record
		return s.cause
	}
	return s.Store.Complete(ctx, record)
}

func outboxStoreFixture(t *testing.T, db *gorm.DB) outbox.Store {
	t.Helper()
	store, err := repository.NewOutbox(db)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func outboxOwner(t *testing.T) string {
	t.Helper()
	owner, err := ownerid.New(t.Context(), "outbox-test")
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

func outboxBrokerFixture(ctx context.Context, t *testing.T) (natsjs.Stream, natsjs.Consumer, outbox.Publisher) {
	t.Helper()
	address := os.Getenv("MUSKDEER_TEST_NATS_ADDRESS")
	token := os.Getenv("MUSKDEER_TEST_NATS_TOKEN")
	if address == "" || token == "" {
		t.Fatal("isolated NATS environment is required; run make integration")
	}
	connection, err := nats.Connect("nats://"+address, nats.Token(token), nats.NoReconnect(), nats.Timeout(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(connection.Close)
	admin, err := natsjs.New(connection, natsjs.WithDefaultTimeout(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	name := strings.NewReplacer("-", "_", ".", "_").Replace(outboxOwner(t))
	prefix := "muskdeer.integration." + name
	stream, err := admin.CreateStream(ctx, natsjs.StreamConfig{
		Name:       name,
		Subjects:   []string{prefix + ".>"},
		Storage:    natsjs.FileStorage,
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
	consumer, err := stream.CreateConsumer(ctx, natsjs.ConsumerConfig{
		Durable:       "portfolio_test",
		AckPolicy:     natsjs.AckExplicitPolicy,
		FilterSubject: prefix + "." + outbox.ExecutionSubject,
		MaxAckPending: 1,
		AckWait:       time.Minute,
		MaxDeliver:    5,
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := jetstream.New(ctx, &configv1.JetStream{
		Address:         address,
		Stream:          name,
		SubjectPrefix:   prefix,
		TokenEnv:        "MUSKDEER_TEST_NATS_TOKEN",
		ConnectTimeout:  durationpb.New(time.Second),
		CommandTimeout:  durationpb.New(time.Second),
		MaxInflight:     1,
		MaxMessageBytes: 64 * 1024,
		TlsEnabled:      proto.Bool(false),
	}, "outbox-integration")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	publisher, err := outbox.NewPublisher(client)
	if err != nil {
		t.Fatal(err)
	}
	return stream, consumer, publisher
}
