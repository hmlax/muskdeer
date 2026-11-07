package venue

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/logging"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	executionvenuev1 "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	marketvenuev1 "github.com/hmlax/muskdeer/deer/pb/market/venue/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

// Runtime owns the only SDK writer, with bounded local admission and durable fencing.
type Runtime struct {
	executionvenuev1.UnimplementedExecutionServer
	db      *gorm.DB
	owner   string
	cfg     *venueconfig.RuntimeConfig
	factory *rpcclient.Factory
	logger  *logging.Logger
	gateway gateway.Factory
	store   *Store
	leases  domain.LeaseRepository
	client  *atomic.Pointer[gatewaySession]
	closing *atomic.Bool
	gates   map[string]chan struct{}
	plans   map[string]*atomic.Int64
}

// Dependencies are borrowed once; the worker owns only the adapter connection it dials.
type Dependencies struct {
	DB         *gorm.DB
	OwnerID    string
	RPC        *rpcclient.Factory
	Logger     *logging.Logger
	Gateway    gateway.Factory
	Connection *configv1.VenueConnection
}

// NewRuntime assembles borrowed resources; the worker creates the SDK connection.
func NewRuntime(ctx context.Context, cfg *configv1.BusinessRuntime, deps Dependencies) (*Runtime, error) {
	db, owner, factory, logger, factoryGateway := deps.DB, deps.OwnerID, deps.RPC, deps.Logger, deps.Gateway
	if deps.Connection == nil {
		return nil, ErrInvalidConfig
	}
	settings, err := venueconfig.LoadRuntime(ctx, db, cfg, deps.Connection)
	if err != nil {
		return nil, err
	}
	store, err := NewStore(db, factoryGateway)
	if err != nil {
		return nil, err
	}
	leases, err := repository.NewLeases(db)
	if err != nil {
		return nil, err
	}
	gates := make(map[string]chan struct{})
	plans := make(map[string]*atomic.Int64)
	for _, p := range settings.Venue.Profiles {
		gates[AccountID(p.UserId, p.FundId)] = make(chan struct{}, 1)
		plans[AccountID(p.UserId, p.FundId)] = &atomic.Int64{}
	}
	return &Runtime{
		db:      db,
		owner:   owner,
		cfg:     settings,
		factory: factory,
		logger:  logger,
		gateway: factoryGateway,
		store:   store,
		leases:  leases,
		gates:   gates,
		plans:   plans,
		client:  &atomic.Pointer[gatewaySession]{},
		closing: &atomic.Bool{},
	}, nil
}

// RegisterGRPC exposes only SDK observation and strategy intent reconciliation.
func (r *Runtime) RegisterGRPC(registrar grpc.ServiceRegistrar) {
	executionvenuev1.RegisterExecutionServer(registrar, r)
}

// Workers owns SDK setup, safety observation and shutdown cancellation.
func (r *Runtime) Workers() []deer.Worker {
	return []deer.Worker{
		{
			Name: "execution.venue",
			Run:  r.run,
		},
	}
}

func (r *Runtime) run(ctx context.Context) (result error) {
	client, err := r.gateway.Dial(ctx, Config{
		Target:  r.cfg.Venue.Target,
		Token:   os.Getenv(r.cfg.Venue.TokenEnv),
		Timeout: r.cfg.OperationTimeout,
	}, r.store)
	if err != nil {
		return err
	}
	defer func() {
		if err := client.Close(context.WithoutCancel(ctx)); err != nil {
			if errors.Is(result, context.Canceled) {
				result = err
			} else {
				result = errors.Join(result, err)
			}
		}
	}()
	if err := client.Ping(ctx); err != nil {
		return err
	}
	r.client.Store(&gatewaySession{Client: client})
	defer func() {
		r.closing.Store(true)
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		var cleanupErr error
		for _, profile := range r.cfg.Venue.Profiles {
			gate := r.gates[AccountID(profile.UserId, profile.FundId)]
			select {
			case gate <- struct{}{}:
				err := r.cancelManaged(cleanup, profile)
				<-gate
				cleanupErr = errors.Join(cleanupErr, err)
			case <-cleanup.Done():
				cleanupErr = errors.Join(cleanupErr, cleanup.Err())
			}
		}
		if cleanupErr != nil {
			if errors.Is(result, context.Canceled) {
				result = cleanupErr
			} else {
				result = errors.Join(result, cleanupErr)
			}
		}
	}()
	for ctx.Err() == nil {
		if err := r.renewSyntheticScopes(ctx); err != nil && ctx.Err() == nil {
			if logErr := r.logger.LogAttrs(ctx, slog.LevelWarn, native.StateRecovering, slog.String("strategy", native.SyntheticWorker)); logErr != nil {
				return logErr
			}
		}
		for _, profile := range r.cfg.Venue.Profiles {
			gate := r.gates[AccountID(profile.UserId, profile.FundId)]
			select {
			case gate <- struct{}{}:
				op, cancel := context.WithTimeout(ctx, r.cfg.OperationTimeout)
				err := r.protect(op, profile)
				cancel()
				<-gate
				if err != nil && ctx.Err() == nil {
					if logErr := r.logger.LogAttrs(ctx, slog.LevelWarn, native.MessageInvalid, slog.String("symbol", profile.Symbol)); logErr != nil {
						return logErr
					}
				}
			default:
			}
		}
		if err := native.Wait(ctx, 500*time.Millisecond); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (r *Runtime) acquire(ctx context.Context, profile *commonv1.VenueProfilePolicy) (domain.LeaseToken, error) {
	token, err := r.leases.Acquire(ctx, domain.LeaseKey{
		Kind:      domain.LeaseKindExecution,
		AccountID: AccountID(profile.UserId, profile.FundId),
	}, r.owner, r.cfg.Venue.LeaseTtl.AsDuration())
	if err != nil {
		return domain.LeaseToken{}, err
	}
	return r.leases.Renew(ctx, token, r.cfg.Venue.LeaseTtl.AsDuration())
}

func (r *Runtime) enter(scope *commonv1.VenueScope) (func(), error) {
	if r.closing.Load() || r.client.Load() == nil {
		return nil, native.ErrInvalid
	}
	gate := r.gates[AccountID(scope.GetUserId(), scope.GetFundId())]
	if gate == nil {
		return nil, native.ErrInvalid
	}
	select {
	case gate <- struct{}{}:
		if r.closing.Load() {
			<-gate
			return nil, native.ErrInvalid
		}
		return func() { <-gate }, nil
	default:
		return nil, domain.ErrLeaseHeld
	}
}

// GetSnapshot is read-only and may be served by a healthy observation instance.
func (r *Runtime) GetSnapshot(ctx context.Context, scope *commonv1.VenueScope) (*commonv1.VenueSnapshot, error) {
	if !rpcsecurity.HasService(ctx, rpcsecurity.ServicePortfolio) && !rpcsecurity.HasService(ctx, rpcsecurity.ServiceStrategy) {
		return nil, status.Error(codes.PermissionDenied, native.MessageInvalid)
	}
	profile, err := native.Profile(r.cfg.Venue, scope)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, native.MessageInvalid)
	}
	leave, err := r.enter(scope)
	if err != nil {
		return nil, status.Error(codes.ResourceExhausted, native.MessageInvalid)
	}
	defer leave()
	_, snapshot, err := r.read(ctx, profile)
	if err != nil {
		return nil, status.Error(codes.Unavailable, native.MessageInvalid)
	}
	return snapshot, nil
}

func (r *Runtime) read(ctx context.Context, profile *commonv1.VenueProfilePolicy) (*gateway.Observation, *commonv1.VenueSnapshot, error) {
	scope := Scope{
		UserID: profile.UserId,
		FundID: profile.FundId,
		Symbol: profile.Symbol,
	}
	raw, err := r.client.Load().Read(ctx, scope)
	if err != nil {
		return nil, nil, err
	}
	snapshot := raw.Snapshot
	if snapshot.Market.PriceTick != profile.PriceTick || snapshot.Market.QuantityStep != profile.QuantityStep || profile.Leverage > snapshot.Market.MaximumLeverage {
		return raw, nil, native.ErrInvalid
	}
	return raw, snapshot, nil
}

func (r *Runtime) book(ctx context.Context, symbol string) (*commonv1.VenueBook, error) {
	connection, err := r.factory.Client(rpcclient.Target{Service: rpcsecurity.ServiceMarket})
	if err != nil {
		return nil, err
	}
	return marketvenuev1.NewMarketClient(connection).GetBook(ctx, &marketvenuev1.BookRequest{Symbol: symbol})
}

// Reconcile accepts only the owning strategy's selected-source target intent.
func (r *Runtime) Reconcile(ctx context.Context, plan *executionvenuev1.QuotePlan) (*executionvenuev1.QuoteResult, error) {
	if !rpcsecurity.HasService(ctx, rpcsecurity.ServiceStrategy) {
		return nil, status.Error(codes.PermissionDenied, native.MessageInvalid)
	}
	profile, err := native.Profile(r.cfg.Venue, plan.GetScope())
	if err != nil || len(plan.GetTargets()) == 0 || len(plan.Targets) > r.maximumTargets() || plan.GetStrategy().GetAccountId() != native.StrategyKey(profile.GetUserId(), profile.GetFundId()) {
		return nil, status.Error(codes.InvalidArgument, native.MessageInvalid)
	}
	leave, err := r.enter(plan.Scope)
	if err != nil {
		return nil, status.Error(codes.ResourceExhausted, native.MessageInvalid)
	}
	defer leave()
	result, err := r.reconcile(ctx, profile, plan)
	if err != nil {
		if logErr := r.logger.LogAttrs(context.WithoutCancel(ctx), slog.LevelWarn, native.StateRecovering, slog.String("symbol", profile.Symbol), slog.String("reason", safeReason(err)), slog.String("upstream_code", status.Code(err).String())); logErr != nil {
			return nil, logErr
		}
		return nil, status.Error(codes.Unavailable, native.MessageInvalid)
	}
	return result, nil
}

func (r *Runtime) reconcile(ctx context.Context, profile *commonv1.VenueProfilePolicy, plan *executionvenuev1.QuotePlan) (*executionvenuev1.QuoteResult, error) {
	lease, err := r.acquire(ctx, profile)
	if err != nil {
		return nil, err
	}
	book, err := r.book(ctx, profile.Symbol)
	if err != nil {
		return nil, err
	}
	if err := r.validatePlanAuthority(ctx, profile, plan, book); err != nil {
		return nil, err
	}
	book.ValidUntilMs = min(book.ValidUntilMs, plan.Book.ValidUntilMs)
	snapshot, recoveryErr := r.refresh(ctx, profile, lease, book)
	if recoveryErr != nil && (!errors.Is(recoveryErr, domain.ErrOutcomeUnknown) || snapshot == nil) {
		return nil, recoveryErr
	}
	budget, err := r.store.Budget(ctx, lease.Key.AccountID)
	if err != nil || !budget.Enabled {
		return nil, ErrRiskLimit
	}
	result := &executionvenuev1.QuoteResult{State: native.StateRecovering}
	if recoveryErr != nil {
		return result, recoveryErr
	}
	// Finish the preceding batch before draining another set of live levels.
	// Unresolved work does not renew the previous plan's expiry protection.
	if pending, err := r.unresolvedCancels(ctx, lease.Key.AccountID); err != nil || pending {
		return result, err
	}
	wanted, err := r.targets(profile, plan)
	if err != nil {
		return nil, err
	}
	retained, err := retainQuoteOrders(profile, snapshot, wanted, budget.CutoverCompleted, r.cfg.ReplacementFraction)
	if err != nil {
		return nil, err
	}
	// A current validated plan protects retained, non-crossing bridge quotes
	// while cancellation proofs settle; it does not authorize new submissions.
	r.plans[lease.Key.AccountID].Store(book.ValidUntilMs)
	cycle := &quoteCycle{
		profile:     profile,
		bridgeSlots: r.cfg.BridgeSlots,
		lease:       lease,
		authority: LiveAuthority{
			Strategy:   plan.Strategy,
			Book:       book,
			InstanceID: r.cfg.InstanceID,
			Revision:   r.cfg.Revision,
		},
		targets:  quoteMaintenanceOrder(plan.Targets, snapshot),
		snapshot: snapshot,
		retained: retained,
		result:   result,
	}
	var seedErr error
	if budget.CutoverCompleted {
		seedErr = r.seedQuoteSides(ctx, cycle)
	}
	err = r.cancelObsolete(ctx, profile, lease, snapshot, retained, result, budget.CutoverCompleted)
	if err != nil {
		return nil, errors.Join(err, seedErr)
	}
	if seedErr != nil {
		return result, seedErr
	}
	if result.Cancelled > 0 {
		snapshot, err = r.refresh(ctx, profile, lease, book)
		if err != nil {
			return nil, err
		}
		cycle.snapshot = snapshot
		cycle.admitted = nil
	}
	// All obsolete native IDs must have positive terminal proof, not absence.
	if pending, err := r.unresolvedCancels(ctx, lease.Key.AccountID); err != nil || pending {
		return result, err
	}
	if !budget.CutoverCompleted {
		if err := r.store.CompleteCutover(ctx, lease); err != nil {
			return nil, err
		}
	}
	for _, target := range cycle.targets {
		price, _ := native.Value(target.Price, false, false)
		key := target.Side + "/" + price.String()
		if retained.targets[key] {
			continue
		}
		if !cycle.canPlace(target) {
			retained.pending = true
			continue
		}
		if err := r.submitQuote(ctx, cycle, target); err != nil {
			return nil, err
		}
	}
	if !retained.pending {
		result.State = native.StateRunning
	}
	r.plans[lease.Key.AccountID].Store(book.ValidUntilMs)
	if err := r.logger.LogAttrs(ctx, slog.LevelInfo, result.State, slog.String("symbol", profile.Symbol), slog.Uint64("placed", uint64(result.Placed)), slog.Uint64("cancelled", uint64(result.Cancelled)), slog.Uint64("retained", uint64(result.Retained))); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Runtime) refresh(ctx context.Context, profile *commonv1.VenueProfilePolicy, lease domain.LeaseToken, book *commonv1.VenueBook) (*commonv1.VenueSnapshot, error) {
	raw, snapshot, err := r.read(ctx, profile)
	if err != nil {
		return nil, err
	}
	if r.cfg.InstanceID > 0 {
		if err := r.store.Observe(ctx, lease, snapshot); err != nil {
			return nil, err
		}
		return snapshot, r.syncSnapshotBudget(ctx, lease, snapshot, book)
	}
	clients, err := r.store.pendingClientIDs(ctx, lease.Key.AccountID)
	if err != nil {
		return nil, err
	}
	var recoveryErr error
	if len(clients) != 0 {
		orders, err := r.client.Load().FindClientOrders(ctx, Scope{
			UserID: profile.UserId,
			FundID: profile.FundId,
			Symbol: profile.Symbol,
		}, clients, int32(raw.BasePrecision))
		if err != nil {
			return nil, err
		}
		if err := r.store.Observe(ctx, lease, &commonv1.VenueSnapshot{
			Scope:  snapshot.Scope,
			Orders: orders,
		}); err != nil {
			return nil, err
		}
		if len(orders) < len(clients) {
			recoveryErr = domain.ErrOutcomeUnknown
		}
	}
	pending, err := r.store.PendingIDs(ctx, lease.Key.AccountID)
	if err != nil {
		return nil, err
	}
	present := make(map[string]bool)
	for _, order := range snapshot.Orders {
		present[order.OrderId] = true
	}
	for _, id := range pending {
		if present[strconv.FormatUint(id, 10)] {
			continue
		}
		order, err := r.client.Load().FindKnown(ctx, Scope{
			UserID: profile.UserId,
			FundID: profile.FundId,
			Symbol: profile.Symbol,
		}, id, int32(raw.BasePrecision))
		if err != nil {
			if errors.Is(err, domain.ErrOutcomeUnknown) {
				recoveryErr = domain.ErrOutcomeUnknown
				continue // Keep its reservation and any healthy existing quotes.
			}
			return nil, err
		}
		// Commit each positive native fact before querying the next ID. A full
		// cutover history can exceed one operation deadline; later calls must
		// continue from the remaining IDs rather than repeat the whole prefix.
		if err := r.store.Observe(ctx, lease, &commonv1.VenueSnapshot{
			Scope:  snapshot.Scope,
			Orders: []*commonv1.VenueOrder{order},
		}); err != nil {
			return nil, err
		}
		snapshot.Orders = append(snapshot.Orders, order)
	}
	if err := r.store.Observe(ctx, lease, snapshot); err != nil {
		return nil, err
	}
	if err := r.syncSnapshotBudget(ctx, lease, snapshot, book); err != nil {
		return nil, err
	}
	return snapshot, recoveryErr
}

func (r *Runtime) syncSnapshotBudget(ctx context.Context, lease domain.LeaseToken, snapshot *commonv1.VenueSnapshot, book *commonv1.VenueBook) error {
	mark, err := native.Value(snapshot.Market.MarkPrice, false, false)
	if err != nil {
		return err
	}
	if len(book.GetAsks()) != 0 {
		upper, err := native.Value(book.Asks[len(book.Asks)-1].Price, false, false)
		if err != nil {
			return err
		}
		mark = decimal.Max(mark, upper)
	}
	gross := decimal.Zero
	for _, position := range snapshot.Positions {
		quantity, err := native.Value(position.SignedQuantity, true, true)
		if err != nil {
			return err
		}
		gross = gross.Add(quantity.Abs().Mul(mark))
	}
	for _, order := range snapshot.Orders {
		if !native.Active(order.Status) {
			continue
		}
		price, _ := native.Value(order.Price, false, false)
		quantity, _ := native.Value(order.Quantity, false, false)
		executed, _ := native.Value(order.ExecutedQuantity, false, true)
		gross = gross.Add(quantity.Sub(executed).Mul(decimal.Max(price, mark)))
	}
	if r.cfg.InstanceID > 0 {
		return r.store.syncBudget(ctx, lease, snapshot, gross, true)
	}
	return r.store.SyncBudget(ctx, lease, snapshot, gross)
}

func (r *Runtime) cancelOne(ctx context.Context, lease domain.LeaseToken, profile *commonv1.VenueProfilePolicy, order *commonv1.VenueOrder) error {
	id, err := strconv.ParseUint(order.OrderId, 10, 64)
	if err != nil {
		return err
	}
	admission, err := r.store.PrepareCancel(ctx, lease, CancelRequest{
		Scope: Scope{
			UserID: profile.UserId,
			FundID: profile.FundId,
			Symbol: profile.Symbol,
		},
		OrderID: id,
	})
	if err != nil {
		return err
	}
	return r.client.Load().Cancel(ctx, admission)
}

func (r *Runtime) cancelObsolete(ctx context.Context, profile *commonv1.VenueProfilePolicy, lease domain.LeaseToken, snapshot *commonv1.VenueSnapshot, retained *quoteRetention, result *executionvenuev1.QuoteResult, cutover bool) error {
	for _, order := range snapshot.Orders {
		if !native.Active(order.Status) {
			continue
		}
		if !ownsCancellation(profile, order.ClientId, cutover) {
			continue
		}
		if retained.orders[order.OrderId] {
			result.Retained++
			continue
		}
		if err := r.cancelOne(ctx, lease, profile, order); err != nil {
			return err
		}
		result.Cancelled++
	}
	return nil
}
