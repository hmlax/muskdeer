package native

import (
	"context"
	"crypto/rand"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/logging"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	executionvenuev1 "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	marketvenuev1 "github.com/hmlax/muskdeer/deer/pb/market/venue/v1"
	portfoliovenuev1 "github.com/hmlax/muskdeer/deer/pb/portfolio/venue/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/tracing"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	values "github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

const (
	makerTriggerBook = "book"
	makerTriggerPoll = "poll"
)

// directMakerWorker reports whether the worker computes SDK maker targets for
// one configured instance. Grid, long-grid and legacy paths keep polling.
func directMakerWorker(cfg *venueconfig.RuntimeConfig) bool {
	return cfg.InstanceID > 0 && cfg.Template == domain.StrategyTemplateMarketMaker && cfg.Grid == nil && cfg.LongGrid == nil
}

// makerState is one immutable control and account-rules observation. It never
// authorizes an SDK operation: execution re-checks configuration, budget and
// fresh matching state for every admitted plan and every send.
type makerState struct {
	active      bool
	profile     *commonv1.VenueProfilePolicy
	snapshot    *commonv1.VenueSnapshot
	readErr     error
	usableUntil time.Time
}

func (s *makerState) usable(now time.Time) bool {
	return s != nil && s.active && s.profile != nil && now.Before(s.usableUntil)
}

// makerLoop proposes a plan as soon as a newer source frame arrives. Control
// and account reads run beside it at the original poll interval, so a frame no
// longer waits for a poll phase or for database/portfolio round trips. When no
// frame arrives for one interval, the loop falls back to reading the latest
// book, preserving the previous polling cadence as a floor.
type makerLoop struct {
	interval time.Duration
	timeout  time.Duration
	observe  func(context.Context) (*makerState, error)
	book     func(context.Context) (*commonv1.VenueBook, error)
	propose  func(context.Context, *makerState, *commonv1.VenueBook, string) error
	report   func(context.Context, string, error) error
}

func (l *makerLoop) run(ctx context.Context, books <-chan *commonv1.VenueBook) error {
	if l.interval <= 0 || l.timeout <= 0 || l.observe == nil || l.book == nil || l.propose == nil || l.report == nil {
		return values.ErrInvalid
	}
	work, cancel := context.WithCancel(ctx)
	var state atomic.Pointer[makerState]
	fatal := make(chan error, 1)
	var group sync.WaitGroup
	defer func() { cancel(); group.Wait() }()
	group.Go(func() {
		if err := l.refresh(work, &state); err != nil && work.Err() == nil {
			fatal <- err
		}
	})
	fallback := time.NewTimer(l.interval)
	defer fallback.Stop()
	var last int64
	proposed := false
	failed := false
	attempt := func(book *commonv1.VenueBook, trigger string) error {
		current := state.Load()
		if !current.usable(time.Now()) {
			return nil
		}
		op, end := context.WithTimeout(work, l.timeout)
		defer end()
		if book == nil {
			var err error
			if book, err = l.book(op); err != nil {
				return err
			}
			if book == nil {
				return values.ErrInvalid
			}
		}
		last, proposed = book.GetSequence(), true
		return l.propose(op, current, book, trigger)
	}
	for {
		var err error
		select {
		case <-work.Done():
			return ctx.Err()
		case err := <-fatal:
			return err
		case book := <-books:
			if book == nil || proposed && book.GetSequence() <= last || book.GetValidUntilMs() <= time.Now().UnixMilli() {
				continue
			}
			err = attempt(book, makerTriggerBook)
		case <-fallback.C:
			err = attempt(nil, makerTriggerPoll)
		}
		if err != nil && !failed && work.Err() == nil {
			if e := l.report(work, "propose", err); e != nil {
				return e
			}
		}
		failed = err != nil
		if !fallback.Stop() {
			select {
			case <-fallback.C:
			default:
			}
		}
		fallback.Reset(l.interval)
	}
}

// refresh owns the only writer of state. A failed read withdraws the previous
// observation, so proposals stop exactly as a failed poll tick used to.
func (l *makerLoop) refresh(ctx context.Context, state *atomic.Pointer[makerState]) error {
	failed := false
	for ctx.Err() == nil {
		current, err := l.observe(ctx)
		if err != nil {
			state.Store(nil)
			if !failed && ctx.Err() == nil {
				if e := l.report(ctx, "observe", err); e != nil {
					return e
				}
			}
		} else {
			state.Store(current)
		}
		failed = err != nil
		if err := values.Wait(ctx, l.interval); err != nil {
			return nil
		}
	}
	return nil
}

// offerLatestBook keeps one pending frame: an older unprocessed frame is
// replaced, never queued behind the writer.
func offerLatestBook(books chan *commonv1.VenueBook, book *commonv1.VenueBook) {
	select {
	case books <- book:
		return
	default:
	}
	select {
	case <-books:
	default:
	}
	select {
	case books <- book:
	default:
	}
}

func runDirectMaker(ctx context.Context, db *gorm.DB, factory *rpcclient.Factory, cfg *venueconfig.RuntimeConfig, startup *commonv1.VenueProfilePolicy, rules *targetRules, logger *logging.Logger) error {
	books := make(chan *commonv1.VenueBook, 1)
	streamFailures := make(chan error, 1)
	work, cancel := context.WithCancel(ctx)
	var group sync.WaitGroup
	defer func() { cancel(); group.Wait() }()
	group.Go(func() { streamMakerBooks(work, factory, startup.Symbol, cfg.PollInterval, books, streamFailures) })
	report := func(ctx context.Context, step string, _ error) error {
		return logger.LogAttrs(ctx, slog.LevelWarn, values.StateRecovering, slog.String("symbol", startup.Symbol), slog.String("step", step))
	}
	loop := &makerLoop{
		interval: cfg.PollInterval,
		timeout:  cfg.OperationTimeout,
		observe: func(ctx context.Context) (*makerState, error) {
			return observeDirectMaker(ctx, db, factory, cfg, startup)
		},
		book: func(ctx context.Context) (*commonv1.VenueBook, error) {
			conn, err := factory.Client(rpcclient.Target{Service: rpcsecurity.ServiceMarket})
			if err != nil {
				return nil, err
			}
			return marketvenuev1.NewMarketClient(conn).GetBook(ctx, &marketvenuev1.BookRequest{Symbol: startup.Symbol})
		},
		propose: func(ctx context.Context, state *makerState, book *commonv1.VenueBook, trigger string) error {
			return proposeDirectMaker(ctx, factory, cfg, rules, state, book, trigger)
		},
		report: report,
	}
	result := make(chan error, 1)
	group.Go(func() { result <- loop.run(work, books) })
	streamFailed := false
	for {
		select {
		case err := <-result:
			return err
		case err := <-streamFailures:
			// Polling keeps quoting while the stream reconnects; report only the
			// transition into a failing stream.
			if err == nil {
				streamFailed = false
				continue
			}
			if !streamFailed && work.Err() == nil {
				if e := report(work, "subscribe", err); e != nil {
					return e
				}
			}
			streamFailed = true
		}
	}
}

// streamMakerBooks owns the book subscription and reconnects after a bounded
// wait. Errors are forwarded without blocking; nil marks a received frame.
func streamMakerBooks(ctx context.Context, factory *rpcclient.Factory, symbol string, interval time.Duration, books chan *commonv1.VenueBook, failures chan<- error) {
	signal := func(err error) {
		select {
		case failures <- err:
		default:
		}
	}
	for ctx.Err() == nil {
		err := factory.Subscribe(ctx, rpcclient.Target{Service: rpcsecurity.ServiceMarket}, func(work context.Context, conn grpc.ClientConnInterface) error {
			stream, err := marketvenuev1.NewMarketClient(conn).SubscribeBooks(work, &marketvenuev1.BookRequest{Symbol: symbol})
			if err != nil {
				return err
			}
			healthy := false
			for {
				book, err := stream.Recv()
				if err != nil {
					return err
				}
				if !healthy {
					healthy = true
					signal(nil)
				}
				offerLatestBook(books, book)
			}
		})
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = io.EOF
		}
		signal(err)
		if values.Wait(ctx, max(interval, 100*time.Millisecond)) != nil {
			return
		}
	}
}

func observeDirectMaker(ctx context.Context, db *gorm.DB, factory *rpcclient.Factory, cfg *venueconfig.RuntimeConfig, startup *commonv1.VenueProfilePolicy) (*makerState, error) {
	started := time.Now()
	op, cancel := context.WithTimeout(ctx, cfg.OperationTimeout)
	defer cancel()
	active, err := instanceconfig.Active(op, db, cfg.InstanceID, cfg.Revision)
	if err != nil {
		return nil, err
	}
	state := &makerState{
		active:      active,
		usableUntil: started.Add(cfg.PollInterval + cfg.OperationTimeout),
	}
	if !active {
		return state, nil
	}
	if state.profile, err = venueconfig.CurrentProfile(op, db, startup); err != nil {
		return nil, err
	}
	conn, err := factory.Client(rpcclient.Target{Service: rpcsecurity.ServicePortfolio})
	if err != nil {
		return nil, err
	}
	state.snapshot, state.readErr = portfoliovenuev1.NewPortfolioClient(conn).GetSnapshot(op, values.Scope(state.profile))
	return state, nil
}

func proposeDirectMaker(ctx context.Context, factory *rpcclient.Factory, cfg *venueconfig.RuntimeConfig, rules *targetRules, state *makerState, book *commonv1.VenueBook, trigger string) (result error) {
	ctx, span := factory.StartOperation(ctx, "strategy.quote")
	span.SetAttributes(attribute.String("market.symbol", state.profile.Symbol), attribute.String("strategy.trigger", trigger))
	defer func() { tracing.RecordError(ctx, result); span.End() }()
	targets, err := rules.targets(ctx, cfg, state.profile, book, state.snapshot, state.readErr)
	if err != nil {
		return err
	}
	conn, err := factory.Client(rpcclient.Target{Service: rpcsecurity.ServiceExecution})
	if err != nil {
		return err
	}
	plan := &executionvenuev1.QuotePlan{
		Scope:              values.Scope(state.profile),
		Book:               book,
		Strategy:           &commonv1.VenueAuthority{AccountId: values.StrategyKey(state.profile.UserId, state.profile.FundId)},
		Targets:            targets,
		RequestId:          rand.Text(),
		StrategyInstanceId: cfg.InstanceID,
		StrategyRevision:   cfg.Revision,
		ProposedAtUnixNano: time.Now().UnixNano(),
	}
	span.SetAttributes(attribute.String("quote.target.id", plan.RequestId), attribute.Int64("market.source.sequence", book.Sequence))
	_, err = executionvenuev1.NewExecutionClient(conn).Reconcile(ctx, plan)
	return err
}
