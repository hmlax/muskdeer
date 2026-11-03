// Package native owns rebuildable SDK fund observations in the portfolio process.
package native

import (
	"context"
	"errors"
	"log/slog"
	"strings"
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
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	himalayav1 "github.com/hmlax/muskdeer/deer/proto/rpc/himalaya/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	values "github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
)

// Runtime polls authoritative execution observations; it never simulates money.
type Runtime struct {
	himalayav1.UnimplementedPortfolioServer
	db      *gorm.DB
	owner   string
	cfg     *configv1.BusinessRuntime
	factory *rpcclient.Factory
	leases  domain.LeaseRepository
	logger  *logging.Logger
}

// NewRuntime borrows the existing pool and RPC factory without starting tasks.
func NewRuntime(db *gorm.DB, owner string, cfg *configv1.BusinessRuntime, factory *rpcclient.Factory, logger *logging.Logger) (*Runtime, error) {
	leases, err := repository.NewLeases(db)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		db:      db,
		owner:   owner,
		cfg:     proto.CloneOf(cfg),
		factory: factory,
		leases:  leases,
		logger:  logger,
	}, nil
}

// RegisterGRPC exposes current scoped observations only.
func (r *Runtime) RegisterGRPC(registrar grpc.ServiceRegistrar) {
	himalayav1.RegisterPortfolioServer(registrar, r)
}

// Workers owns one bounded projection loop per fund.
func (r *Runtime) Workers() []deer.Worker {
	var result []deer.Worker
	for _, profile := range r.cfg.Himalaya.Profiles {
		result = append(result, deer.Worker{
			Name: "portfolio.native." + strings.ToLower(profile.Symbol),
			Run: func(ctx context.Context) error {
				failed := false
				for ctx.Err() == nil {
					op, cancel := context.WithTimeout(ctx, r.cfg.OperationTimeout.AsDuration())
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

func (r *Runtime) poll(ctx context.Context, profile *configv1.HimalayaProfile) error {
	token, err := r.leases.Acquire(ctx, domain.LeaseKey{
		Kind:      domain.LeaseKindExecution,
		AccountID: "hp/" + values.ScopeKey(profile.UserId, profile.FundId),
	}, r.owner, r.cfg.Himalaya.LeaseTtl.AsDuration())
	if err != nil {
		return err
	}
	token, err = r.leases.Renew(ctx, token, r.cfg.Himalaya.LeaseTtl.AsDuration())
	if err != nil {
		return err
	}
	connection, err := r.factory.Client(rpcclient.Target{Service: rpcsecurity.ServiceExecution})
	if err != nil {
		return err
	}
	snapshot, err := himalayav1.NewExecutionClient(connection).GetSnapshot(ctx, values.Scope(profile))
	if err != nil {
		return err
	}
	if snapshot.ValidUntilMs <= time.Now().UnixMilli() || snapshot.Scope.UserId != profile.UserId || snapshot.Scope.FundId != profile.FundId || snapshot.Scope.Symbol != profile.Symbol {
		return values.ErrInvalid
	}
	return r.db.WithContext(ctx).Model(&modelv1.HimalayaProjection{}).Transaction(func(tx *gorm.DB) error {
		if _, err := repository.LockNativeAuthority(ctx, tx, values.Authority(token)); err != nil {
			return err
		}
		var row modelv1.HimalayaProjection
		err := tx.WithContext(ctx).Model(&modelv1.HimalayaProjection{}).Where("account_id = ?", values.ScopeKey(profile.UserId, profile.FundId)).First(&row).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		fresh := errors.Is(err, gorm.ErrRecordNotFound)
		row.AccountId = values.ScopeKey(profile.UserId, profile.FundId)
		row.Snapshot, row.PublisherOwnerId, row.PublisherEpoch = proto.CloneOf(snapshot), token.OwnerID, token.Epoch
		if fresh {
			return tx.WithContext(ctx).Model(&modelv1.HimalayaProjection{}).Create(&row).Error
		}
		return tx.WithContext(ctx).Model(&modelv1.HimalayaProjection{}).Where("id = ?", row.Id).
			Select("Snapshot", "PublisherOwnerId", "PublisherEpoch").Updates(&row).Error
	})
}

// GetSnapshot returns no stale or unfenced observation as current state.
func (r *Runtime) GetSnapshot(ctx context.Context, scope *commonv1.HimalayaScope) (*commonv1.HimalayaSnapshot, error) {
	if !rpcsecurity.HasService(ctx, rpcsecurity.ServiceStrategy) && !rpcsecurity.HasService(ctx, rpcsecurity.ServiceExecution) {
		return nil, status.Error(codes.PermissionDenied, values.MessageInvalid)
	}
	if _, err := values.Profile(r.cfg.Himalaya, scope); err != nil {
		return nil, status.Error(codes.InvalidArgument, values.MessageInvalid)
	}
	var row modelv1.HimalayaProjection
	if err := r.db.WithContext(ctx).Model(&modelv1.HimalayaProjection{}).Where("account_id = ? AND delete_at IS NULL", values.ScopeKey(scope.UserId, scope.FundId)).First(&row).Error; err != nil {
		return nil, status.Error(codes.Unavailable, values.MessageInvalid)
	}
	if row.Snapshot.GetValidUntilMs() <= time.Now().UnixMilli() || repository.CheckNativeAuthority(ctx, r.db, &commonv1.HimalayaAuthority{
		AccountId: "hp/" + row.AccountId,
		OwnerId:   row.PublisherOwnerId,
		Epoch:     row.PublisherEpoch,
	}) != nil {
		return nil, status.Error(codes.Unavailable, values.MessageInvalid)
	}
	return proto.CloneOf(row.Snapshot), nil
}
