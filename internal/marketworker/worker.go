// Package marketworker owns one feed's publication lease and stream lifecycle.
package marketworker

import (
	"context"
	"errors"
	"time"

	"github.com/hmlax/muskdeer/deer/ownerid"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketbook"
	"github.com/hmlax/muskdeer/internal/marketfeed"
	"github.com/hmlax/muskdeer/internal/marketquality"
	"github.com/hmlax/muskdeer/internal/marketwire"
)

// Fixed lifecycle failures never expose private network or database details.
const (
	MessageConfig = "market worker configuration is invalid"
	MessageEnded  = "market stream ended before cancellation"
)

// Worker errors terminate the owning Deer; only held standby ownership waits.
var (
	ErrConfig = errors.New(MessageConfig)
	ErrEnded  = errors.New(MessageEnded)
)

// Stream owns/join its own I/O tasks and invokes the observer serially.
type Stream interface {
	Run(context.Context, marketfeed.Observer) error
}

// Factory starts no task. It is invoked only after acquiring publisher authority.
type Factory func(domain.MarketFeedDefinition) (Stream, error)

// Config contains immutable, bounded owned settings, not a retained context.
type Config struct {
	Feed             domain.MarketFeedDefinition
	OwnerID          string
	LeaseTTL         time.Duration
	OperationTimeout time.Duration
	RetryInterval    time.Duration
}

// Dependencies are borrowed; Worker owns only its renewal and stream tasks.
type Dependencies struct {
	Leases    domain.LeaseRepository
	Snapshots domain.MarketRepository
	Factory   Factory
}

// Worker runs one actor and an owned renewal loop while it holds a feed lease.
type Worker struct {
	config Config
	deps   Dependencies
}

// New does no I/O or task creation. Bindings and policy are immutable values.
func New(config Config, deps Dependencies) (*Worker, error) {
	if config.Feed.ID <= 0 || config.Feed.Validate() != nil || !ownerid.Valid(config.OwnerID) ||
		config.LeaseTTL < domain.MinLeaseTTL || config.LeaseTTL > domain.MaxLeaseTTL || config.LeaseTTL%time.Millisecond != 0 ||
		config.OperationTimeout < 10*time.Millisecond || config.OperationTimeout > 5*time.Second || config.OperationTimeout*2 >= config.LeaseTTL ||
		config.RetryInterval < 10*time.Millisecond || config.RetryInterval > 5*time.Second ||
		deps.Leases == nil || deps.Snapshots == nil || deps.Factory == nil {
		return nil, ErrConfig
	}
	return &Worker{
		config: config,
		deps:   deps,
	}, nil
}

// Run waits as a standby without opening a stream. Once acquired, any renewal,
// publication or stream failure stops this owner instead of silently restarting.
func (w *Worker) Run(ctx context.Context) error {
	if ctx == nil {
		return ErrConfig
	}
	key := domain.LeaseKey{
		Kind:         domain.LeaseKindMarket,
		MarketFeedID: w.config.Feed.ID,
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		call, cancel := context.WithTimeout(ctx, w.config.OperationTimeout)
		token, err := w.deps.Leases.Acquire(call, key, w.config.OwnerID, w.config.LeaseTTL)
		cancel()
		if errors.Is(err, domain.ErrLeaseHeld) {
			if err := delay(ctx, w.config.RetryInterval); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		return w.owned(ctx, token)
	}
}

func (w *Worker) owned(parent context.Context, token domain.LeaseToken) (result error) {
	defer func() {
		// Release is best effort within a small independent cleanup budget; its
		// failure is returned. Stored expiry still bounds authority if DB is down.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(parent), min(w.config.OperationTimeout, 250*time.Millisecond))
		defer cancel()
		result = errors.Join(result, w.deps.Leases.Release(cleanup, token))
	}()
	tracker, revision, err := w.restore(parent)
	if err != nil {
		return err
	}
	stream, err := w.deps.Factory(w.config.Feed)
	if err != nil {
		return err
	}
	if stream == nil {
		return ErrConfig
	}
	ctx, cancel := context.WithCancelCause(parent)
	finished := make(chan error, 1)
	go func() {
		err := w.renew(ctx, token)
		if err != nil {
			cancel(err)
		}
		finished <- err
	}()
	joined := false
	defer func() {
		cancel(context.Canceled)
		if !joined {
			result = errors.Join(result, <-finished)
		}
	}()
	observer := &publicationActor{
		worker:   w,
		tracker:  tracker,
		token:    token,
		revision: revision,
	}
	runErr := stream.Run(ctx, observer.observe)
	cause := context.Cause(ctx)
	cancel(context.Canceled)
	renewErr := <-finished
	joined = true
	if parent.Err() != nil {
		return parent.Err()
	}
	if cause != nil && !errors.Is(cause, context.Canceled) {
		return cause
	}
	if renewErr != nil && !errors.Is(renewErr, context.Canceled) {
		return renewErr
	}
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) {
			return errors.Join(ErrEnded, runErr)
		}
		return runErr
	}
	return ErrEnded
}

func (w *Worker) restore(ctx context.Context) (*marketquality.Tracker, int64, error) {
	feed := w.config.Feed
	tracker, err := marketquality.New(feed.Source, marketwire.Instrument{
		ID:     feed.InstrumentID,
		Symbol: feed.Symbol,
	}, feed.Mode, feed.Policy)
	if err != nil {
		return nil, 0, err
	}
	call, cancel := context.WithTimeout(ctx, w.config.OperationTimeout)
	defer cancel()
	previous, err := w.deps.Snapshots.Get(call, feed.ID)
	if errors.Is(err, domain.ErrMarketMissing) {
		return tracker, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	if previous.FeedID != feed.ID || previous.Revision <= 0 {
		return nil, 0, domain.ErrMarketIdentity
	}
	if err := tracker.Restore(previous.Baseline, time.Now()); err != nil {
		return nil, 0, err
	}
	return tracker, previous.Revision, nil
}

func (w *Worker) renew(ctx context.Context, token domain.LeaseToken) error {
	ticker := time.NewTicker(w.config.LeaseTTL / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-ticker.C:
			call, cancel := context.WithTimeout(ctx, w.config.OperationTimeout)
			_, err := w.deps.Leases.Renew(call, token, w.config.LeaseTTL)
			cancel()
			if err != nil {
				return err
			}
		}
	}
}

type publicationActor struct {
	worker     *Worker
	tracker    *marketquality.Tracker
	token      domain.LeaseToken
	revision   int64
	connection int64
	generation int64
	sequence   int64
}

func (a *publicationActor) observe(ctx context.Context, observation marketfeed.Observation) error {
	feed := a.worker.config.Feed
	if observation.Source != feed.Source || observation.InstrumentID != feed.InstrumentID || observation.Symbol != feed.Symbol || observation.Connection <= 0 || observation.Connection < a.connection {
		return domain.ErrMarketIdentity
	}
	now := time.Now().UTC()
	var reading domain.MarketReading
	if observation.State == marketbook.StateReady {
		if observation.Reason != marketfeed.ReasonNone {
			return domain.ErrUnreliableMarket
		}
		var err error
		reading, err = a.tracker.Observe(observation.View, observation.Connection, now)
		if err != nil {
			return err
		}
	} else {
		reason, err := observationReason(observation)
		if err != nil {
			return err
		}
		reading = a.tracker.Invalidate(reason, now)
		reading.Connection, reading.Generation, reading.Sequence = observation.Connection, a.generation, a.sequence
		if observation.Connection > a.connection {
			reading.Generation, reading.Sequence = 0, 0
		}
	}
	call, cancel := context.WithTimeout(ctx, a.worker.config.OperationTimeout)
	defer cancel()
	publication, err := a.worker.deps.Snapshots.Publish(call, a.token, a.revision, reading)
	if err != nil {
		return err
	}
	return a.accept(publication, reading)
}

func (a *publicationActor) accept(publication domain.MarketPublication, reading domain.MarketReading) error {
	feed := a.worker.config.Feed
	if publication.FeedID != feed.ID || publication.OwnerID != a.token.OwnerID || publication.OwnerEpoch != a.token.Epoch || publication.Revision != a.revision+1 {
		return domain.ErrMarketIdentity
	}
	if publication.Reading.Source != feed.Source || publication.Reading.InstrumentID != feed.InstrumentID || publication.Reading.Mode != feed.Mode || publication.Reading.Symbol != feed.Symbol || publication.Reading.Validate() != nil {
		return domain.ErrMarketIdentity
	}
	if publication.Reading.Quality != reading.Quality || publication.Reading.Reason != reading.Reason {
		// Queue age or DB-time policy can reject a locally healthy candidate.
		// Only the persisted accepted baseline may survive that rejection.
		tracker, err := marketquality.New(feed.Source, marketwire.Instrument{
			ID:     feed.InstrumentID,
			Symbol: feed.Symbol,
		}, feed.Mode, feed.Policy)
		if err != nil {
			return err
		}
		if err := tracker.Restore(publication.Baseline, time.Now()); err != nil {
			return err
		}
		a.tracker = tracker
	}
	a.revision, a.connection = publication.Revision, reading.Connection
	a.generation, a.sequence = reading.Generation, reading.Sequence
	return nil
}

func observationReason(observation marketfeed.Observation) (domain.MarketReason, error) {
	if observation.State == marketbook.StateSynchronizing && observation.Reason == marketfeed.ReasonNone {
		return domain.MarketReasonSynchronizing, nil
	}
	if observation.State != marketbook.StateInvalid {
		return domain.MarketReasonNone, domain.ErrUnreliableMarket
	}
	switch observation.Reason {
	case marketfeed.ReasonTransport:
		return domain.MarketReasonDisconnected, nil
	case marketfeed.ReasonSequence:
		return domain.MarketReasonSequence, nil
	case marketfeed.ReasonPayload:
		return domain.MarketReasonPayload, nil
	case marketfeed.ReasonOverflow:
		return domain.MarketReasonOverflow, nil
	default:
		return domain.MarketReasonNone, domain.ErrUnreliableMarket
	}
}

func delay(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
