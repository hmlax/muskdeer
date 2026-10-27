package marketworker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/goleak"

	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketbook"
	"github.com/hmlax/muskdeer/internal/marketfeed"
	"github.com/hmlax/muskdeer/internal/marketquality"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

type streamFunc func(context.Context, marketfeed.Observer) error

func (f streamFunc) Run(ctx context.Context, observer marketfeed.Observer) error {
	return f(ctx, observer)
}

type leaseFixture struct {
	acquireError error
	renewError   error
	acquired     chan struct{}
	renewed      chan struct{}
	acquireOnce  sync.Once
	renewOnce    sync.Once
	blockRenew   bool
	renewDone    chan struct{}
	mu           sync.Mutex
	released     int
	streamDone   <-chan struct{}
}

func (f *leaseFixture) Acquire(ctx context.Context, key domain.LeaseKey, owner string, ttl time.Duration) (domain.LeaseToken, error) {
	if _, bounded := ctx.Deadline(); !bounded {
		return domain.LeaseToken{}, ErrConfig
	}
	f.acquireOnce.Do(func() { close(f.acquired) })
	return domain.LeaseToken{
		Key:       key,
		OwnerID:   owner,
		Epoch:     1,
		ExpiresAt: time.Now().Add(ttl),
	}, f.acquireError
}

func (f *leaseFixture) Renew(ctx context.Context, token domain.LeaseToken, _ time.Duration) (domain.LeaseToken, error) {
	if _, bounded := ctx.Deadline(); !bounded {
		return domain.LeaseToken{}, ErrConfig
	}
	f.renewOnce.Do(func() { close(f.renewed) })
	if f.blockRenew {
		defer close(f.renewDone)
		<-ctx.Done()
		return domain.LeaseToken{}, ctx.Err()
	}
	return token, f.renewError
}

func (f *leaseFixture) Release(ctx context.Context, _ domain.LeaseToken) error {
	if _, bounded := ctx.Deadline(); !bounded || ctx.Err() != nil {
		return ErrConfig
	}
	if f.streamDone != nil {
		select {
		case <-f.streamDone:
		default:
			return ErrEnded
		}
	}
	if f.renewDone != nil {
		select {
		case <-f.renewDone:
		default:
			return ErrEnded
		}
	}
	f.mu.Lock()
	f.released++
	f.mu.Unlock()
	return nil
}

type snapshotFixture struct {
	mu         sync.Mutex
	feed       domain.MarketFeedDefinition
	rows       []domain.MarketPublication
	coerceOnce bool
	coerced    bool
	baseline   domain.MarketBaseline
}

func (f *snapshotFixture) Get(_ context.Context, _ int64) (domain.MarketPublication, error) {
	return domain.MarketPublication{}, domain.ErrMarketMissing
}

func (f *snapshotFixture) Publish(ctx context.Context, token domain.LeaseToken, revision int64, reading domain.MarketReading) (domain.MarketPublication, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, bounded := ctx.Deadline(); !bounded || revision != int64(len(f.rows)) {
		return domain.MarketPublication{}, domain.ErrMarketRevision
	}
	if f.baseline.Source == domain.MarketSourceUnspecified {
		f.baseline = domain.MarketBaseline{
			Source:        f.feed.Source,
			InstrumentID:  f.feed.InstrumentID,
			Symbol:        f.feed.Symbol,
			Mode:          f.feed.Mode,
			SourceVersion: f.feed.SourceVersion,
		}
	}
	if reading.Quality == domain.MarketQualityHealthy {
		if f.coerceOnce && !f.coerced {
			f.coerced = true
			reading.Quality, reading.Reason = domain.MarketQualityStale, domain.MarketReasonAge
			reading.Bid, reading.Ask, reading.Mid = decimal.Zero, decimal.Zero, decimal.Zero
			reading.BidQuantity, reading.AskQuantity = decimal.Zero, decimal.Zero
		} else {
			f.baseline.Mid, f.baseline.AcceptedAt = reading.Mid, reading.EvaluatedAt
		}
	}
	value := domain.MarketPublication{
		MarketReference: domain.MarketReference{
			FeedID:         f.feed.ID,
			OwnerID:        token.OwnerID,
			OwnerEpoch:     token.Epoch,
			AuthorityUntil: token.ExpiresAt,
			Revision:       revision + 1,
			Reading:        reading,
		},
		Baseline: f.baseline,
	}
	f.rows = append(f.rows, value)
	return value, nil
}

func TestStandbyDoesNotCreateStreamOrRenewAnotherOwner(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	leases := leaseFixtureNew()
	leases.acquireError = domain.ErrLeaseHeld
	config := configFixture()
	created := false
	worker, err := New(config, Dependencies{
		Leases:    leases,
		Snapshots: &snapshotFixture{feed: config.Feed},
		Factory: func(domain.MarketFeedDefinition) (Stream, error) {
			created = true
			return nil, ErrConfig
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- worker.Run(ctx) }()
	select {
	case <-leases.acquired:
	case <-ctx.Done():
		t.Fatal("standby did not attempt bounded acquisition")
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) || created {
		t.Fatalf("standby opened another owner's stream: %v", err)
	}
	if leases.released != 0 {
		t.Fatal("standby released an unowned lease")
	}
}

func TestRenewalFailureCancelsAndJoinsStreamBeforeRelease(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	leases := leaseFixtureNew()
	leases.renewError = domain.ErrLeaseLost
	stopped := make(chan struct{})
	leases.streamDone = stopped
	config := configFixture()
	worker, err := New(config, Dependencies{
		Leases:    leases,
		Snapshots: &snapshotFixture{feed: config.Feed},
		Factory: func(domain.MarketFeedDefinition) (Stream, error) {
			return streamFunc(func(ctx context.Context, _ marketfeed.Observer) error {
				defer close(stopped)
				<-ctx.Done()
				return ctx.Err()
			}), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Run(ctx); !errors.Is(err, domain.ErrLeaseLost) || leases.released != 1 {
		t.Fatalf("lease loss did not join/release in order: %v", err)
	}
}

func TestReconnectResetsNativeGenerationAndPreservesSerializedPublication(t *testing.T) {
	config := configFixture()
	store := &snapshotFixture{feed: config.Feed}
	leases := leaseFixtureNew()
	worker, err := New(config, Dependencies{
		Leases:    leases,
		Snapshots: store,
		Factory: func(feed domain.MarketFeedDefinition) (Stream, error) {
			return streamFunc(func(ctx context.Context, observe marketfeed.Observer) error {
				for _, observation := range []marketfeed.Observation{
					controlFixture(feed, 1),
					observationFixture(feed, 1, 5, 99, "100"),
					controlFixture(feed, 2),
					observationFixture(feed, 2, 1, 1, "100"),
				} {
					if err := observe(ctx, observation); err != nil {
						return err
					}
				}
				return nil
			}), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Run(t.Context()); !errors.Is(err, ErrEnded) {
		t.Fatal(err)
	}
	if len(store.rows) != 4 || store.rows[2].Reading.Connection != 2 || store.rows[2].Reading.Generation != 0 || store.rows[2].Reading.Sequence != 0 || store.rows[3].Reading.Generation != 1 {
		t.Fatal("new connection inherited previous book sequence")
	}
}

func TestDatabaseRejectedPriceCannotPoisonLocalAcceptedBaseline(t *testing.T) {
	config := configFixture()
	store := &snapshotFixture{
		feed:       config.Feed,
		coerceOnce: true,
	}
	worker, err := New(config, Dependencies{
		Leases:    leaseFixtureNew(),
		Snapshots: store,
		Factory: func(feed domain.MarketFeedDefinition) (Stream, error) {
			return streamFunc(func(ctx context.Context, observe marketfeed.Observer) error {
				for index, mid := range []string{"100", "100", "200", "200"} {
					if err := observe(ctx, observationFixture(feed, 1, 1, int64(index+1), mid)); err != nil {
						return err
					}
				}
				return nil
			}), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Run(t.Context()); !errors.Is(err, ErrEnded) {
		t.Fatal(err)
	}
	if len(store.rows) != 4 || store.rows[3].Reading.Quality != domain.MarketQualityHealthy || store.rows[3].Reading.Mid.String() != "200" {
		t.Fatal("database-unaccepted price poisoned local baseline")
	}
}

func TestUnrequestedStreamCancellationIsAnUnexpectedEnd(t *testing.T) {
	config := configFixture()
	worker, err := New(config, Dependencies{
		Leases:    leaseFixtureNew(),
		Snapshots: &snapshotFixture{feed: config.Feed},
		Factory: func(domain.MarketFeedDefinition) (Stream, error) {
			return streamFunc(func(context.Context, marketfeed.Observer) error { return context.Canceled }), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Run(t.Context()); !errors.Is(err, ErrEnded) {
		t.Fatal("unrequested child cancellation looked like normal service stop")
	}
}

func TestStreamPanicStillJoinsOwnedRenewalBeforeRelease(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	config := configFixture()
	config.LeaseTTL, config.OperationTimeout = time.Second, 100*time.Millisecond
	leases := leaseFixtureNew()
	leases.blockRenew, leases.renewDone = true, make(chan struct{})
	worker, err := New(config, Dependencies{
		Leases:    leases,
		Snapshots: &snapshotFixture{feed: config.Feed},
		Factory: func(domain.MarketFeedDefinition) (Stream, error) {
			return streamFunc(func(context.Context, marketfeed.Observer) error {
				<-leases.renewed
				panic("fixture stream panic")
			}), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	panicked := false
	func() {
		defer func() { panicked = recover() != nil }()
		_ = worker.Run(ctx)
	}()
	if !panicked || leases.released != 1 {
		t.Fatal("stream panic escaped before joining renewal and releasing ownership")
	}
}

func leaseFixtureNew() *leaseFixture {
	return &leaseFixture{
		acquired: make(chan struct{}),
		renewed:  make(chan struct{}),
	}
}

func configFixture() Config {
	policy := marketquality.DefaultPolicy()
	policy.RecoveryWindow, policy.RecoverySamples = 0, 2
	return Config{
		Feed: domain.MarketFeedDefinition{
			ID:            1,
			Source:        domain.MarketSourceBinance,
			InstrumentID:  1,
			Symbol:        "BTCUSDT",
			Mode:          domain.MarketModeReplay,
			SourceVersion: domain.MarketSourceVersion,
			Policy:        policy,
		},
		OwnerID:          "market.01234567890123456789012345678901",
		LeaseTTL:         300 * time.Millisecond,
		OperationTimeout: 50 * time.Millisecond,
		RetryInterval:    10 * time.Millisecond,
	}
}

func controlFixture(feed domain.MarketFeedDefinition, connection int64) marketfeed.Observation {
	return marketfeed.Observation{
		Source:       feed.Source,
		InstrumentID: feed.InstrumentID,
		Symbol:       feed.Symbol,
		Connection:   connection,
		State:        marketbook.StateSynchronizing,
		Reason:       marketfeed.ReasonNone,
	}
}

func observationFixture(feed domain.MarketFeedDefinition, connection, generation, sequence int64, mid string) marketfeed.Observation {
	now := time.Now().UTC()
	price := decimal.RequireFromString(mid)
	return marketfeed.Observation{
		Source:       feed.Source,
		InstrumentID: feed.InstrumentID,
		Symbol:       feed.Symbol,
		Connection:   connection,
		State:        marketbook.StateReady,
		Reason:       marketfeed.ReasonNone,
		View: marketbook.View{
			Source:       feed.Source,
			InstrumentID: feed.InstrumentID,
			Symbol:       feed.Symbol,
			Generation:   generation,
			Sequence:     sequence,
			Bid: marketbook.Level{
				Price:    price.Sub(decimal.RequireFromString("0.01")),
				Quantity: decimal.NewFromInt(1),
			},
			Ask: marketbook.Level{
				Price:    price.Add(decimal.RequireFromString("0.01")),
				Quantity: decimal.NewFromInt(1),
			},
			Mid:        price,
			EventTime:  now.Add(-time.Millisecond),
			ReceivedAt: now,
		},
	}
}
