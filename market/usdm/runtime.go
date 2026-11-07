package usdm

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/logging"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	marketvenuev1 "github.com/hmlax/muskdeer/deer/pb/market/venue/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

// Runtime publishes only its selected, currently owned native futures books.
type Runtime struct {
	inventory []*commonv1.VenueInstanceRevision
	marketvenuev1.UnimplementedMarketServer
	children map[string]*Runtime
	db       *gorm.DB
	owner    string
	cfg      *venueconfig.RuntimeConfig
	leases   domain.LeaseRepository
	mu       sync.RWMutex
	books    map[string]*commonv1.VenueBook
	trades   map[string]*commonv1.VenueTrade
	logger   *logging.Logger
}

// NewRuntime borrows dependencies without starting sockets or tasks.
func NewRuntime(ctx context.Context, db *gorm.DB, owner string, cfg *configv1.BusinessRuntime, logger *logging.Logger) (*Runtime, error) {
	settings, err := venueconfig.LoadRuntime(ctx, db, cfg, nil)
	if err != nil {
		return nil, err
	}
	leases, err := repository.NewLeases(db)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		db:     db,
		owner:  owner,
		cfg:    settings,
		leases: leases,
		books:  make(map[string]*commonv1.VenueBook),
		trades: make(map[string]*commonv1.VenueTrade),
		logger: logger,
	}, nil
}

// RegisterGRPC exposes only native market reads.
func (r *Runtime) RegisterGRPC(registrar grpc.ServiceRegistrar) {
	marketvenuev1.RegisterMarketServer(registrar, r)
}

// Workers has one independently owned session per configured instrument.
func (r *Runtime) Workers() []deer.Worker {
	if r.children != nil {
		var workers []deer.Worker
		for _, key := range sortedDependencyKeys(r.children) {
			workers = append(workers, r.children[key].Workers()...)
		}
		return workers
	}
	var result []deer.Worker
	for _, profile := range r.cfg.Venue.Profiles {
		result = append(result, deer.Worker{
			Name: "market.usdm." + strings.ToLower(profile.Symbol),
			Run:  func(ctx context.Context) error { return r.run(ctx, profile.Symbol) },
		})
	}
	if r.cfg.Venue.GetSyntheticTape().GetEnabled() && r.cfg.Venue.SyntheticTape.Mode == configv1.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE {
		for _, profile := range r.cfg.Venue.SyntheticTape.Profiles {
			result = append(result, deer.Worker{
				Name: tradeWorkerPrefix + strings.ToLower(profile.Symbol),
				Run:  func(ctx context.Context) error { return r.runTrades(ctx, profile.Symbol) },
			})
		}
	}
	return result
}

// GetBook verifies both publication freshness and retained database ownership.
func (r *Runtime) GetBook(ctx context.Context, request *marketvenuev1.BookRequest) (*commonv1.VenueBook, error) {
	if r.children != nil {
		if child := r.children[request.GetSymbol()]; child != nil {
			return child.GetBook(ctx, request)
		}
		return nil, status.Error(codes.InvalidArgument, native.MessageInvalid)
	}
	if !rpcsecurity.HasService(ctx, rpcsecurity.ServiceStrategy) && !rpcsecurity.HasService(ctx, rpcsecurity.ServiceExecution) {
		return nil, status.Error(codes.PermissionDenied, native.MessageInvalid)
	}
	r.mu.RLock()
	book := proto.CloneOf(r.books[request.GetSymbol()])
	r.mu.RUnlock()
	if book == nil || book.ValidUntilMs <= time.Now().UnixMilli() || repository.CheckNativeAuthority(ctx, r.db, book.Publisher) != nil {
		return nil, status.Error(codes.Unavailable, native.MessageInvalid)
	}
	return book, nil
}

func (r *Runtime) run(ctx context.Context, symbol string) error {
	for ctx.Err() == nil {
		if err := r.session(ctx, symbol); err != nil && ctx.Err() == nil {
			if logErr := r.logger.LogAttrs(ctx, slog.LevelWarn, native.StateRecovering, slog.String("symbol", symbol)); logErr != nil {
				return logErr
			}
		}
		r.mu.Lock()
		delete(r.books, symbol)
		r.mu.Unlock()
		if err := native.Wait(ctx, time.Second); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (r *Runtime) session(ctx context.Context, symbol string) error {
	op, cancel := context.WithTimeout(ctx, r.cfg.OperationTimeout)
	token, err := r.leases.Acquire(op, domain.LeaseKey{
		Kind:      domain.LeaseKindExecution,
		AccountID: native.MarketKey(symbol),
	}, r.owner, r.cfg.Venue.LeaseTtl.AsDuration())
	cancel()
	if err != nil {
		return err
	}
	feed, err := NewFeed(symbol)
	if err != nil {
		return err
	}
	return feed.Run(ctx, func(work context.Context, book *commonv1.VenueBook) error {
		if time.Until(token.ExpiresAt) < r.cfg.Venue.LeaseTtl.AsDuration()/2 {
			bounded, cancel := context.WithTimeout(work, r.cfg.OperationTimeout)
			defer cancel()
			token, err = r.leases.Renew(bounded, token, r.cfg.Venue.LeaseTtl.AsDuration())
			if err != nil {
				return err
			}
		}
		if err := work.Err(); err != nil {
			return err
		}
		book.Publisher = native.Authority(token)
		book.ValidUntilMs = min(book.ValidUntilMs, token.ExpiresAt.UnixMilli()-50)
		if book.ValidUntilMs <= time.Now().UnixMilli() {
			return domain.ErrLeaseLost
		}
		r.mu.Lock()
		r.books[symbol] = proto.CloneOf(book)
		r.mu.Unlock()
		return nil
	})
}
