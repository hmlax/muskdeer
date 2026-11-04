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
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	himalayav1 "github.com/hmlax/muskdeer/deer/proto/rpc/himalaya/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
)

// Runtime publishes only its selected, currently owned native futures books.
type Runtime struct {
	himalayav1.UnimplementedMarketServer
	db     *gorm.DB
	owner  string
	cfg    *configv1.BusinessRuntime
	leases domain.LeaseRepository
	mu     sync.RWMutex
	books  map[string]*commonv1.HimalayaBook
	trades map[string]*commonv1.HimalayaTrade
	logger *logging.Logger
}

// NewRuntime borrows dependencies without starting sockets or tasks.
func NewRuntime(db *gorm.DB, owner string, cfg *configv1.BusinessRuntime, logger *logging.Logger) (*Runtime, error) {
	leases, err := repository.NewLeases(db)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		db:     db,
		owner:  owner,
		cfg:    proto.CloneOf(cfg),
		leases: leases,
		books:  make(map[string]*commonv1.HimalayaBook),
		trades: make(map[string]*commonv1.HimalayaTrade),
		logger: logger,
	}, nil
}

// RegisterGRPC exposes only native market reads.
func (r *Runtime) RegisterGRPC(registrar grpc.ServiceRegistrar) {
	himalayav1.RegisterMarketServer(registrar, r)
}

// Workers has one independently owned session per configured instrument.
func (r *Runtime) Workers() []deer.Worker {
	var result []deer.Worker
	for _, profile := range r.cfg.Himalaya.Profiles {
		result = append(result, deer.Worker{
			Name: "market.usdm." + strings.ToLower(profile.Symbol),
			Run:  func(ctx context.Context) error { return r.run(ctx, profile.Symbol) },
		})
	}
	if r.cfg.Himalaya.GetSyntheticTape().GetEnabled() && r.cfg.Himalaya.SyntheticTape.Mode == configv1.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE {
		for _, profile := range r.cfg.Himalaya.SyntheticTape.Profiles {
			result = append(result, deer.Worker{
				Name: tradeWorkerPrefix + strings.ToLower(profile.Symbol),
				Run:  func(ctx context.Context) error { return r.runTrades(ctx, profile.Symbol) },
			})
		}
	}
	return result
}

// GetBook verifies both publication freshness and retained database ownership.
func (r *Runtime) GetBook(ctx context.Context, request *himalayav1.BookRequest) (*commonv1.HimalayaBook, error) {
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
	op, cancel := context.WithTimeout(ctx, r.cfg.OperationTimeout.AsDuration())
	token, err := r.leases.Acquire(op, domain.LeaseKey{
		Kind:      domain.LeaseKindExecution,
		AccountID: native.MarketKey(symbol),
	}, r.owner, r.cfg.Himalaya.LeaseTtl.AsDuration())
	cancel()
	if err != nil {
		return err
	}
	feed, err := NewFeed(symbol)
	if err != nil {
		return err
	}
	return feed.Run(ctx, func(work context.Context, book *commonv1.HimalayaBook) error {
		if time.Until(token.ExpiresAt) < r.cfg.Himalaya.LeaseTtl.AsDuration()/2 {
			bounded, cancel := context.WithTimeout(work, r.cfg.OperationTimeout.AsDuration())
			defer cancel()
			token, err = r.leases.Renew(bounded, token, r.cfg.Himalaya.LeaseTtl.AsDuration())
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
