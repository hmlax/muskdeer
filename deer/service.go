package deer

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"

	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/transport"
	kratosgrpc "github.com/go-kratos/kratos/v2/transport/grpc"
	"google.golang.org/grpc"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/jetstream"
	"github.com/hmlax/muskdeer/deer/logging"
	"github.com/hmlax/muskdeer/deer/metrics"
	"github.com/hmlax/muskdeer/deer/nacos"
	"github.com/hmlax/muskdeer/deer/postgres"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/redis"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
)

// ErrPostgresNotConfigured reports a required database missing from service configuration.
var ErrPostgresNotConfigured = errors.New("postgres is not configured")

// ErrRedisNotConfigured reports a required cache missing from service configuration.
var ErrRedisNotConfigured = errors.New("redis is not configured")

// ErrJetStreamNotConfigured reports a required event publisher missing from configuration.
var ErrJetStreamNotConfigured = errors.New("jetstream is not configured")

// ErrNacosNotConfigured reports a required naming dependency missing from configuration.
var ErrNacosNotConfigured = errors.New("nacos is not configured")

// ErrGRPCClientsNotConfigured reports a missing outbound RPC factory.
var ErrGRPCClientsNotConfigured = errors.New("gRPC clients are not configured")

// ErrNotReady reports a service that has not started or is stopping.
var ErrNotReady = errors.New("service is not ready")

// ServiceSpec supplies process-owned dependencies and business RPC registration.
// Callbacks run synchronously during construction; they must not retain the server
// for later mutation or start unowned goroutines.
type ServiceSpec struct {
	Resources    []Initializer
	RegisterGRPC func(context.Context, *Deer, grpc.ServiceRegistrar) error
	// Workers configures blocking tasks after dependencies, before listeners.
	// It must not start tasks or acquire resources outside this owner.
	Workers   func(context.Context, *Deer) ([]Worker, error)
	LogOutput io.Writer // Borrowed stdout override; nil selects os.Stdout.
	// SetupProcessLogging is only for a sole process entry, before SDK use.
	// Its cleanup runs after dependencies and before the owned logger closes.
	SetupProcessLogging func(context.Context, *logging.Logger) (Cleanup, error)
}

// Deer owns one independent Kratos service, its listeners and configured dependencies.
// Dependencies supplied through With options remain owned by the caller.
// Construct once, Run at most once, and defer the returned Cleanup immediately.
// Run also releases all resources before returning.
type Deer struct {
	app               *kratos.App
	cfg               *configv1.ServiceConfig
	servers           []*managedTransport
	workers           []*managedWorker
	release           Cleanup
	grpcAddress       string
	monitoringAddress string
	ready             atomic.Bool
	readySignal       chan struct{}
	stopSignal        chan struct{}
	done              chan struct{}
	mu                sync.Mutex
	started           bool
	closed            bool
	stopOnce          sync.Once
	closeErr          error
	logger            *logging.Logger
	database          *postgres.Client
	cache             *redis.Client
	events            *jetstream.Client
	discovery         *nacos.Client
	publication       *nacos.Publication
	rpcClients        *rpcclient.Factory
	metrics           *metrics.Registry
}

// NewDeer constructs a service with an optional gRPC listener. Acquisition and
// registration failures roll back dependencies and listeners in reverse order.
// With options borrow initialized clients instead of opening configured replacements.
func NewDeer(ctx context.Context, cfg *configv1.ServiceConfig, spec ServiceSpec, opts ...Option) (*Deer, Cleanup, error) {
	return newDeer(ctx, cfg, spec, false, opts...)
}

// GRPCAddress returns the actual reserved address, or empty if gRPC is disabled.
func (d *Deer) GRPCAddress() string { return d.grpcAddress }

// Logger returns this instance's initialized JSON logger, borrowed by consumers.
// A WithLogger caller retains ownership and closes it only after Deer cleanup.
func (d *Deer) Logger() *logging.Logger { return d.logger }

// DB returns the configured or injected GORM handle without opening a connection.
// Repositories must use WithContext(ctx).Model(&generatedModel{}) for data access.
func (d *Deer) DB() (*gorm.DB, error) {
	if d.database == nil {
		return nil, ErrPostgresNotConfigured
	}
	return d.database.GORM(), nil
}

// Redis returns the initialized cache, borrowed by consumers. Only its owner
// (Deer or the WithRedis caller) may close it, after this service's work stops.
func (d *Deer) Redis() (*redis.Client, error) {
	if d.cache == nil {
		return nil, ErrRedisNotConfigured
	}
	return d.cache, nil
}

// JetStream returns the initialized synchronous publisher, borrowed by consumers.
// A WithJetStream caller closes it only after Deer cleanup.
func (d *Deer) JetStream() (*jetstream.Client, error) {
	if d.events == nil {
		return nil, ErrJetStreamNotConfigured
	}
	return d.events, nil
}

// Discovery returns a borrowed, initialized Nacos client for fresh lookups.
// A WithDiscovery caller closes it only after Deer cleanup.
func (d *Deer) Discovery() (*nacos.Client, error) {
	if d.discovery == nil {
		return nil, ErrNacosNotConfigured
	}
	return d.discovery, nil
}

// CheckReady verifies listener state and configured or injected dependencies live.
// Ready alone reports lifecycle state; health endpoints should use CheckReady.
func (d *Deer) CheckReady(ctx context.Context) error {
	if ctx == nil {
		return errors.New("readiness context is required")
	}
	if !d.Ready() {
		return ErrNotReady
	}
	if d.database != nil {
		if err := d.database.Ping(ctx); err != nil {
			return err
		}
	}
	if d.cache != nil {
		if err := d.cache.Ping(ctx); err != nil {
			return err
		}
	}
	if d.events != nil {
		if err := d.events.Ping(ctx); err != nil {
			return err
		}
	}
	if d.discovery != nil {
		if err := d.discovery.Ping(ctx); err != nil {
			return err
		}
	}
	if d.rpcClients != nil {
		if err := d.rpcClients.Health(); err != nil {
			return err
		}
	}
	if d.publication != nil {
		if err := d.publication.Health(); err != nil {
			return err
		}
	}
	for _, worker := range d.workers {
		if err := worker.check(ctx); err != nil {
			return err
		}
	}
	if !d.Ready() {
		return ErrNotReady
	}
	return ctx.Err()
}

// Ready reports whether all transports have started and shutdown has not begun.
func (d *Deer) Ready() bool { return d.ready.Load() }

// WaitReady waits for startup, termination or caller cancellation.
func (d *Deer) WaitReady(ctx context.Context) error {
	if ctx == nil {
		return errors.New("readiness context is required")
	}
	select {
	case <-d.readySignal:
		if d.Ready() {
			return nil
		}
		return errors.New("service is stopping")
	case <-d.done:
		return errors.New("service terminated before readiness")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Run blocks until cancellation, cleanup, a process signal or transport failure.
// Cancellation returns ctx.Err after stopping transports and releasing resources.
func (d *Deer) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("run context is required")
	}
	d.mu.Lock()
	if d.started || d.closed {
		d.mu.Unlock()
		return errors.New("service can only run once before cleanup")
	}
	d.started = true
	d.mu.Unlock()
	runErr, stopErr := d.run(ctx)
	d.finish(ctx, stopErr)
	return errors.Join(runErr, d.closeErr)
}

func newDeer(ctx context.Context, cfg *configv1.ServiceConfig, spec ServiceSpec, api bool, opts ...Option) (*Deer, Cleanup, error) {
	if ctx == nil {
		return nil, nil, errors.New("construction context is required")
	}
	cfg, err := validateConfig(cfg, api)
	if err != nil {
		return nil, nil, err
	}
	if cfg.Grpc == nil && spec.RegisterGRPC != nil {
		return nil, nil, errors.New("RPC registration requires a gRPC listener")
	}
	settings, err := resolveOptions(opts)
	if err != nil {
		return nil, nil, err
	}
	startup, cancel := context.WithTimeout(ctx, cfg.StartupTimeout.AsDuration())
	defer cancel()
	release, err := settings.initialize(startup, cfg, spec)
	if err != nil {
		return nil, nil, err
	}
	d := &Deer{
		cfg:         cfg,
		release:     release,
		readySignal: make(chan struct{}),
		stopSignal:  make(chan struct{}),
		done:        make(chan struct{}),
		logger:      settings.logger,
		database:    settings.database,
		cache:       settings.cache,
		events:      settings.events,
		discovery:   settings.discovery,
		rpcClients:  settings.rpcClients,
		metrics:     metrics.New(),
	}
	if err := d.addWorkers(startup, spec.Workers); err != nil {
		return nil, nil, errors.Join(err, d.cleanup(ctx))
	}
	if cfg.Grpc != nil {
		if err := d.addGRPC(startup, spec.RegisterGRPC); err != nil {
			return nil, nil, errors.Join(err, d.cleanup(ctx))
		}
	}
	if cfg.Monitoring != nil {
		if err := d.addMonitoring(startup); err != nil {
			return nil, nil, errors.Join(err, d.cleanup(ctx))
		}
	}
	if err := d.configurePublication(); err != nil {
		return nil, nil, errors.Join(err, d.cleanup(ctx))
	}
	if err := startup.Err(); err != nil {
		return nil, nil, errors.Join(err, d.cleanup(ctx))
	}
	return d, d.cleanup, nil
}

func (d *Deer) addGRPC(ctx context.Context, register func(context.Context, *Deer, grpc.ServiceRegistrar) error) error {
	var security *tls.Config
	if d.cfg.GrpcSecurity != nil {
		var err error
		security, err = rpcsecurity.Server(d.cfg.GrpcSecurity, d.cfg.Name+grpcServiceSuffix)
		if err != nil {
			return err
		}
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", d.cfg.Grpc.Address)
	if err != nil {
		return fmt.Errorf("listen gRPC: %w", err)
	}
	accepting := &acceptingListener{
		Listener: listener,
		ready:    make(chan struct{}),
	}
	server := kratosgrpc.NewServer(
		kratosgrpc.Listener(accepting),
		kratosgrpc.TLSConfig(security),
		kratosgrpc.Timeout(d.cfg.Grpc.RequestTimeout.AsDuration()),
		kratosgrpc.DisableReflection(),
		kratosgrpc.CustomHealth(),
		kratosgrpc.UnaryInterceptor(d.measureUnary, requestUnaryInterceptor),
		kratosgrpc.Options(grpc.WaitForHandlers(true)),
		kratosgrpc.StreamInterceptor(d.measureStream, requestStreamInterceptor, streamDeadlineInterceptor(d.cfg.Grpc.RequestTimeout.AsDuration())),
	)
	d.servers = append(d.servers, newManagedTransport(server, accepting, "grpc", d.requestStop))
	d.grpcAddress = listener.Addr().String()
	healthv1.RegisterHealthServer(server, &readinessHealth{
		owner:      d,
		watchSlots: make(chan struct{}, maximumHealthWatches),
	})
	if register != nil {
		if err := register(ctx, d, server); err != nil {
			return fmt.Errorf("register gRPC: %w", err)
		}
	}
	return nil
}

func (d *Deer) run(ctx context.Context) (error, error) {
	owned, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	servers := make([]transport.Server, 0, len(d.servers)+len(d.workers))
	for _, server := range d.servers {
		servers = append(servers, server)
	}
	for _, worker := range d.workers {
		servers = append(servers, worker)
	}
	d.app = kratos.New(
		kratos.Context(owned),
		kratos.Name(d.cfg.Name),
		kratos.ID(d.cfg.InstanceId),
		kratos.Version(d.cfg.Version),
		kratos.Server(servers...),
		kratos.StopTimeout(d.cfg.ShutdownTimeout.AsDuration()),
		kratos.AfterStart(d.markReady),
		kratos.BeforeStop(func(ctx context.Context) error {
			d.requestStop()
			return d.stopPublication(ctx)
		}),
	)
	// Run owns this single supervisor and joins it before releasing resources.
	completed := make(chan error, 1)
	go func() { completed <- d.app.Run() }()
	var runErr error
	appDone := false
	var publicationFailures <-chan error
	if d.publication != nil {
		publicationFailures = d.publication.Failures()
	}
	select {
	case runErr = <-completed:
		appDone = true
	case <-ctx.Done():
		runErr = ctx.Err()
	case <-d.stopSignal:
	case runErr = <-publicationFailures:
	}
	d.requestStop()
	stopErr := d.app.Stop()
	if stopErr != nil {
		cancel() // A failed stop hook must not leave the supervisor running.
	}
	if !appDone {
		runErr = errors.Join(runErr, <-completed)
	}
	shutdown, shutdownCancel := context.WithTimeout(context.WithoutCancel(ctx), d.cfg.ShutdownTimeout.AsDuration())
	defer shutdownCancel()
	stopErr = errors.Join(stopErr, d.stopOwned(shutdown))
	return runErr, stopErr
}

func (d *Deer) markReady(ctx context.Context) error {
	startup, cancel := context.WithTimeout(ctx, d.cfg.StartupTimeout.AsDuration())
	defer cancel()
	for _, server := range d.servers {
		if err := server.waitReady(startup); err != nil {
			return err
		}
	}
	for _, worker := range d.workers {
		if err := worker.waitReady(startup); err != nil {
			return err
		}
	}
	if d.database != nil {
		if err := d.database.Ping(startup); err != nil {
			return err
		}
	}
	if d.cache != nil {
		if err := d.cache.Ping(startup); err != nil {
			return err
		}
	}
	if d.events != nil {
		if err := d.events.Ping(startup); err != nil {
			return err
		}
	}
	if d.discovery != nil {
		if err := d.discovery.Ping(startup); err != nil {
			return err
		}
	}
	if d.rpcClients != nil {
		if err := d.rpcClients.Health(); err != nil {
			return err
		}
	}
	if d.publication != nil {
		if err := d.publication.Start(startup); err != nil {
			return err
		}
	}
	if err := startup.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	select {
	case <-d.stopSignal:
		return errors.New("service stopped during startup")
	default:
	}
	d.ready.Store(true)
	close(d.readySignal)
	return nil
}

func (d *Deer) cleanup(ctx context.Context) error {
	if ctx == nil {
		return errors.New("cleanup context is required")
	}
	d.mu.Lock()
	unused := !d.started && !d.closed
	if unused {
		d.closed = true
	}
	d.mu.Unlock()
	if unused {
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), d.cfg.ShutdownTimeout.AsDuration())
		defer cancel()
		stopErr := d.stopOwned(shutdown)
		d.finish(ctx, stopErr)
	} else {
		d.requestStop()
	}
	<-d.done
	return d.closeErr
}

func (d *Deer) requestStop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ready.Store(false)
	d.stopOnce.Do(func() { close(d.stopSignal) })
}

func (d *Deer) finish(ctx context.Context, stopErr error) {
	d.ready.Store(false)
	stopErr = errors.Join(stopErr, d.stopPublication(ctx))
	d.closeErr = errors.Join(stopErr, d.release(ctx))
	close(d.done)
}
