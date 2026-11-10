// Package native owns rebuildable SDK fund observations in the portfolio process.
package native

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync/atomic"
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
	executionvenuev1 "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	portfoliomodelv1 "github.com/hmlax/muskdeer/deer/pb/portfolio/model/v1"
	portfoliovenuev1 "github.com/hmlax/muskdeer/deer/pb/portfolio/venue/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	values "github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

// Runtime polls authoritative execution observations; it never simulates money.
type Runtime struct {
	inventory []*commonv1.VenueInstanceRevision
	portfoliovenuev1.UnimplementedPortfolioServer
	children   map[string]*Runtime
	db         *gorm.DB
	owner      string
	cfg        *venueconfig.RuntimeConfig
	factory    *rpcclient.Factory
	leases     domain.LeaseRepository
	logger     *logging.Logger
	memoryOnly bool
	latest     atomic.Pointer[commonv1.VenueSnapshot]
}

// NewRuntime borrows the existing pool and RPC factory without starting tasks.
func NewRuntime(ctx context.Context, db *gorm.DB, owner string, cfg *configv1.BusinessRuntime, factory *rpcclient.Factory, logger *logging.Logger) (*Runtime, error) {
	settings, err := venueconfig.LoadRuntime(ctx, db, cfg, nil)
	if err != nil {
		return nil, err
	}
	leases, err := repository.NewLeases(db)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		db:      db,
		owner:   owner,
		cfg:     settings,
		factory: factory,
		leases:  leases,
		logger:  logger,
	}, nil
}

// RegisterGRPC exposes current scoped observations only.
func (r *Runtime) RegisterGRPC(registrar grpc.ServiceRegistrar) {
	portfoliovenuev1.RegisterPortfolioServer(registrar, r)
}

// Workers owns one bounded projection loop per fund.
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
			Name: "portfolio.native." + strconv.FormatInt(profile.UserId, 10) + "." + strconv.FormatInt(profile.FundId, 10),
			Run: func(ctx context.Context) error {
				failed := false
				for ctx.Err() == nil {
					op, cancel := context.WithTimeout(ctx, r.cfg.OperationTimeout)
					err := r.poll(op, profile)
					cancel()
					if err != nil && !failed && ctx.Err() == nil {
						if e := r.logger.LogAttrs(ctx, slog.LevelWarn, values.StateRecovering, slog.String("symbol", profile.Symbol)); e != nil {
							return e
						}
					}
					failed = err != nil
					if err := values.Wait(ctx, 300*time.Millisecond); err != nil {
						return err
					}
				}
				return ctx.Err()
			},
		})
	}
	return result
}

func (r *Runtime) poll(ctx context.Context, profile *commonv1.VenueProfilePolicy) error {
	var token domain.LeaseToken
	if r.cfg.InstanceID == 0 {
		var err error
		token, err = values.KeepLease(ctx, r.leases, domain.LeaseKey{
			Kind:      domain.LeaseKindExecution,
			AccountID: values.PortfolioKey(profile.UserId, profile.FundId),
		}, r.owner, r.cfg.Venue.LeaseTtl.AsDuration())
		if err != nil {
			return err
		}
	}
	connection, err := r.factory.Client(rpcclient.Target{Service: rpcsecurity.ServiceExecution})
	if err != nil {
		return err
	}
	snapshot, err := executionvenuev1.NewExecutionClient(connection).GetSnapshot(ctx, values.Scope(profile))
	if err != nil {
		return err
	}
	return r.saveSnapshot(ctx, profile, token, snapshot)
}

func (r *Runtime) saveSnapshot(ctx context.Context, profile *commonv1.VenueProfilePolicy, token domain.LeaseToken, snapshot *commonv1.VenueSnapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if snapshot.GetValidUntilMs() <= time.Now().UnixMilli() || snapshot.GetScope().GetUserId() != profile.UserId || snapshot.GetScope().GetFundId() != profile.FundId || snapshot.GetScope().GetSymbol() != profile.Symbol {
		return values.ErrInvalid
	}
	if r.memoryOnly {
		r.latest.Store(proto.CloneOf(snapshot))
		return nil
	}
	return r.db.WithContext(ctx).Model(&portfoliomodelv1.VenueAccountSnapshot{}).Transaction(func(tx *gorm.DB) error {
		if r.cfg.InstanceID == 0 {
			if _, err := repository.LockNativeAuthority(ctx, tx, values.Authority(token)); err != nil {
				return err
			}
		}
		var row portfoliomodelv1.VenueAccountSnapshot
		err := tx.WithContext(ctx).Model(&portfoliomodelv1.VenueAccountSnapshot{}).Where("account_id = ?", values.ScopeKey(profile.UserId, profile.FundId)).First(&row).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		fresh := errors.Is(err, gorm.ErrRecordNotFound)
		row.AccountId = values.ScopeKey(profile.UserId, profile.FundId)
		if profile.StrategyInstanceId != 0 {
			row.Symbol = profile.Symbol
		}
		row.Snapshot, row.PublisherOwnerId, row.PublisherEpoch = proto.CloneOf(snapshot), token.OwnerID, token.Epoch
		if fresh {
			return tx.WithContext(ctx).Model(&portfoliomodelv1.VenueAccountSnapshot{}).Create(&row).Error
		}
		return tx.WithContext(ctx).Model(&portfoliomodelv1.VenueAccountSnapshot{}).Where("id = ?", row.Id).
			Select("Snapshot", "PublisherOwnerId", "PublisherEpoch").Updates(&row).Error
	})
}

// GetSnapshot returns no stale or unfenced observation as current state.
func (r *Runtime) GetSnapshot(ctx context.Context, scope *commonv1.VenueScope) (*commonv1.VenueSnapshot, error) {
	if r.children != nil {
		if child := r.children[values.ScopeKey(scope.GetUserId(), scope.GetFundId())]; child != nil {
			return child.GetSnapshot(ctx, scope)
		}
		return nil, status.Error(codes.InvalidArgument, values.MessageInvalid)
	}
	if !rpcsecurity.HasService(ctx, rpcsecurity.ServiceStrategy) && !rpcsecurity.HasService(ctx, rpcsecurity.ServiceExecution) {
		return nil, status.Error(codes.PermissionDenied, values.MessageInvalid)
	}
	if _, err := values.Profile(r.cfg.Venue, scope); err != nil {
		return nil, status.Error(codes.InvalidArgument, values.MessageInvalid)
	}
	if r.memoryOnly {
		snapshot := r.latest.Load()
		if snapshot.GetValidUntilMs() <= time.Now().UnixMilli() || snapshot.GetScope().GetUserId() != scope.UserId || snapshot.GetScope().GetFundId() != scope.FundId || snapshot.GetScope().GetSymbol() != scope.Symbol {
			return nil, status.Error(codes.Unavailable, values.MessageInvalid)
		}
		return proto.CloneOf(snapshot), nil
	}
	var row portfoliomodelv1.VenueAccountSnapshot
	if err := r.db.WithContext(ctx).Model(&portfoliomodelv1.VenueAccountSnapshot{}).Where("account_id = ? AND delete_at IS NULL", values.ScopeKey(scope.UserId, scope.FundId)).First(&row).Error; err != nil {
		return nil, status.Error(codes.Unavailable, values.MessageInvalid)
	}
	if row.Snapshot.GetValidUntilMs() <= time.Now().UnixMilli() || r.cfg.InstanceID == 0 && repository.CheckNativeAuthority(ctx, r.db, &commonv1.VenueAuthority{
		AccountId: values.PortfolioKey(scope.UserId, scope.FundId),
		OwnerId:   row.PublisherOwnerId,
		Epoch:     row.PublisherEpoch,
	}) != nil {
		return nil, status.Error(codes.Unavailable, values.MessageInvalid)
	}
	return proto.CloneOf(row.Snapshot), nil
}
