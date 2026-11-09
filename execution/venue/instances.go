package venue

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	executionv1 "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

// Instances dispatches immutable per-instance settings through one SDK session.
// Fund gates and current matching observations are shared across algorithms.
type Instances struct {
	inventory []*commonv1.VenueInstanceRevision
	executionv1.UnimplementedExecutionServer
	children   map[int64]*Runtime
	ordered    []*Runtime
	connection *configv1.VenueConnection
	gateway    gateway.Factory
	timeout    time.Duration
}

// NewInstances assembles one shared SDK session without starting work.
func NewInstances(ctx context.Context, instances []*instanceconfig.Instance, deps Dependencies) (*Instances, error) {
	if ctx == nil || deps.Connection == nil || deps.DB == nil || deps.OwnerID == "" || deps.Gateway == nil {
		return nil, ErrInvalidConfig
	}
	store, err := NewMatchingStore(deps.DB, deps.Gateway)
	if err != nil {
		return nil, err
	}
	result := &Instances{
		children:   make(map[int64]*Runtime),
		connection: deps.Connection,
		gateway:    deps.Gateway,
		timeout:    time.Second,
	}
	client, closing := &atomic.Pointer[gatewaySession]{}, &atomic.Bool{}
	gates := make(map[string]chan struct{})
	for _, instance := range instances {
		result.inventory = append(result.inventory, &commonv1.VenueInstanceRevision{
			Id:       instance.ID,
			Revision: instance.Revision,
		})
		if instance.ID <= 0 || result.children[instance.ID] != nil {
			return nil, ErrInvalidConfig
		}
		settings, err := instance.Settings()
		if err != nil {
			return nil, err
		}
		settings.Venue.Target, settings.Venue.TokenEnv = deps.Connection.Target, deps.Connection.TokenEnv
		account := instance.Profile
		key := AccountID(account.UserId, account.FundId)
		if gates[key] == nil {
			gates[key] = make(chan struct{}, 1)
		}
		child := &Runtime{
			db:      deps.DB,
			owner:   deps.OwnerID,
			cfg:     settings,
			factory: deps.RPC,
			logger:  deps.Logger,
			gateway: deps.Gateway,
			store:   store,
			client:  client,
			closing: closing,
			gates:   gates,
			plans:   map[string]*atomic.Int64{key: {}},
		}
		result.children[instance.ID] = child
		result.ordered = append(result.ordered, child)
		result.timeout = max(result.timeout, settings.OperationTimeout)
	}
	return result, nil
}

// RegisterGRPC exposes instance-scoped venue execution.
func (r *Instances) RegisterGRPC(registrar grpc.ServiceRegistrar) {
	executionv1.RegisterExecutionServer(registrar, r)
}

// Workers returns the lifecycle owner of the shared SDK session.
func (r *Instances) Workers() []deer.Worker {
	return []deer.Worker{{
		Name: "execution.venue.instances",
		Run:  r.run,
	}}
}

func (r *Instances) run(ctx context.Context) (result error) {
	if len(r.ordered) == 0 {
		<-ctx.Done()
		return ctx.Err()
	}
	client, err := r.gateway.Dial(ctx, Config{
		Target:  r.connection.Target,
		Token:   os.Getenv(r.connection.TokenEnv),
		Timeout: r.timeout,
	}, r.ordered[0].store)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		result = errors.Join(result, client.Close(cleanup))
	}()
	for _, child := range r.ordered {
		if child.directMaker() {
			if _, ok := client.(gateway.QuoteClient); !ok {
				return ErrInvalidConfig
			}
		}
		if child.cfg.Template == domain.StrategyTemplateWashTrade {
			if _, ok := client.(gateway.WashClient); !ok {
				return ErrInvalidConfig
			}
		}
	}
	if err := client.Ping(ctx); err != nil {
		return err
	}
	r.ordered[0].client.Store(&gatewaySession{Client: client})
	group, work := errgroup.WithContext(ctx)
	group.Go(func() error { return r.watchWashControls(work) })
	for _, child := range r.ordered {
		group.Go(func() error { return child.maintainInstance(work) })
	}
	result = group.Wait() // Join all children before cancellation cleanup or Close.
	r.ordered[0].closing.Store(true)
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	for _, child := range r.ordered {
		if child.cfg.Template == domain.StrategyTemplateWashTrade {
			continue
		}
		for _, profile := range child.cfg.Venue.Profiles {
			gate := child.gates[AccountID(profile.UserId, profile.FundId)]
			select {
			case gate <- struct{}{}:
				err := child.cancelManaged(cleanup, profile)
				<-gate
				result = errors.Join(result, err)
			case <-cleanup.Done():
				result = errors.Join(result, cleanup.Err())
			}
		}
	}
	return result
}

func (r *Runtime) maintainInstance(ctx context.Context) error {
	if r.cfg.Template == domain.StrategyTemplateWashTrade {
		<-ctx.Done()
		return ctx.Err()
	}
	failed := false
	for ctx.Err() == nil {
		var err error
		for _, profile := range r.cfg.Venue.Profiles {
			gate := r.gates[AccountID(profile.UserId, profile.FundId)]
			select {
			case gate <- struct{}{}:
				op, cancel := context.WithTimeout(ctx, r.cfg.OperationTimeout)
				err = r.protect(op, profile)
				cancel()
				<-gate
			default:
			}
		}
		if err != nil && !failed && ctx.Err() == nil {
			if logErr := r.logger.LogAttrs(ctx, slog.LevelWarn, native.StateRecovering, slog.String("instance", strconv.FormatInt(r.cfg.InstanceID, 10)), slog.String("reason", safeReason(err))); logErr != nil {
				return logErr
			}
		}
		failed = err != nil
		// Preserve the existing safety-observation cadence. Strategy evaluation
		// still follows its independent database poll parameter.
		if err := native.Wait(ctx, 500*time.Millisecond); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (r *Instances) selected(id, revision int64, synthetic bool) (*Runtime, error) {
	child := r.children[id]
	if child == nil || id <= 0 || child.cfg.Revision != revision || (child.cfg.Template == domain.StrategyTemplateWashTrade) != synthetic {
		return nil, status.Error(codes.FailedPrecondition, native.MessageInvalid)
	}
	return child, nil
}

// Reconcile routes only a current financial strategy instance.
func (r *Instances) Reconcile(ctx context.Context, p *executionv1.QuotePlan) (*executionv1.QuoteResult, error) {
	child, err := r.selected(p.GetStrategyInstanceId(), p.GetStrategyRevision(), false)
	if err != nil {
		return nil, err
	}
	return child.Reconcile(ctx, p)
}

// PublishSynthetic rejects the retired batch protocol on the current server.
func (r *Instances) PublishSynthetic(_ context.Context, _ *commonv1.SyntheticTapePlan) (*commonv1.SyntheticTapeResult, error) {
	return nil, status.Error(codes.Unimplemented, native.MessageInvalid)
}

// WashTrade publishes only from current immutable WashTrade settings.
func (r *Instances) WashTrade(ctx context.Context, p *executionv1.WashTradeRequest) (*commonv1.SyntheticTapeResult, error) {
	child, err := r.selected(p.GetStrategyInstanceId(), p.GetStrategyRevision(), true)
	if err != nil {
		return nil, err
	}
	return child.WashTrade(ctx, p)
}

// GetSnapshot selects a configured observation scope.
func (r *Instances) GetSnapshot(ctx context.Context, scope *commonv1.VenueScope) (*commonv1.VenueSnapshot, error) {
	for _, child := range r.ordered {
		if _, err := native.Profile(child.cfg.Venue, scope); err == nil {
			return child.GetSnapshot(ctx, scope)
		}
	}
	return nil, status.Error(codes.InvalidArgument, native.MessageInvalid)
}
