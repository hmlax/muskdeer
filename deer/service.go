package deer

import (
	"context"
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
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/logging"
	"github.com/hmlax/muskdeer/deer/postgres"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/redis"
)

// ErrPostgresNotConfigured reports a required database missing from service configuration.
var ErrPostgresNotConfigured = errors.New("postgres is not configured")

// ErrRedisNotConfigured reports a required cache missing from service configuration.
var ErrRedisNotConfigured = errors.New("redis is not configured")

// ErrNotReady reports a service that has not started or is stopping.
var ErrNotReady = errors.New("service is not ready")

// ServiceSpec supplies process-owned dependencies and business RPC registration.
// Callbacks run synchronously during construction; they must not retain the server
// for later mutation or start unowned goroutines.
type ServiceSpec struct {
	Resources    []Initializer
	RegisterGRPC func(context.Context, *Deer, grpc.ServiceRegistrar) error
	LogOutput    io.Writer // Borrowed stdout override; nil selects os.Stdout.
}

// Deer owns one independent Kratos service, its listeners and dependencies.
// Construct once, Run at most once, and defer the returned Cleanup immediately.
// Run also releases all resources before returning.
type Deer struct {
	cfg         *configv1.ServiceConfig
	servers     []*managedTransport
	release     Cleanup
	grpcAddress string
	ready       atomic.Bool
	readySignal chan struct{}
	stopSignal  chan struct{}
	done        chan struct{}
	mu          sync.Mutex
	started     bool
	closed      bool
	stopOnce    sync.Once
	closeErr    error
	logger      *logging.Logger
	database    *postgres.Client
	cache       *redis.Client
}

// NewDeer constructs a service with an optional gRPC listener. Acquisition and
// registration failures roll back dependencies and listeners in reverse order.
func NewDeer(ctx context.Context, cfg *configv1.ServiceConfig, spec ServiceSpec) (*Deer, Cleanup, error) {
	return newDeer(ctx, cfg, spec, false)
}

// GRPCAddress returns the actual reserved address, or empty if gRPC is disabled.
func (d *Deer) GRPCAddress() string { return d.grpcAddress }

// Logger returns this instance's eagerly initialized JSON logger, borrowed by
// callers. The service owns its lifetime; callers must not close it themselves.
func (d *Deer) Logger() *logging.Logger { return d.logger }

// DB returns the configured, borrowed GORM handle without opening a connection.
// Repositories must use WithContext(ctx).Model(&generatedModel{}) for data access.
func (d *Deer) DB() (*gorm.DB, error) {
	if d.database == nil {
		return nil, ErrPostgresNotConfigured
	}
	return d.database.GORM(), nil
}

// Redis returns the eagerly initialized cache, borrowed by callers. The service
// owns its lifetime; callers must not close it themselves.
func (d *Deer) Redis() (*redis.Client, error) {
	if d.cache == nil {
		return nil, ErrRedisNotConfigured
	}
	return d.cache, nil
}

// CheckReady verifies current listener state and configured dependencies live.
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

func newDeer(ctx context.Context, cfg *configv1.ServiceConfig, spec ServiceSpec, api bool) (*Deer, Cleanup, error) {
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
	startup, cancel := context.WithTimeout(ctx, cfg.StartupTimeout.AsDuration())
	defer cancel()
	var logger *logging.Logger
	var database *postgres.Client
	var cache *redis.Client
	plan := make([]Initializer, 0, len(spec.Resources)+3)
	plan = append(plan, Initializer{
		Name: "deer.logging",
		Open: func(ctx context.Context) (Cleanup, error) {
			var err error
			logger, err = logging.New(ctx, cfg.Logging, logging.Identity{
				Service:  cfg.Name,
				Instance: cfg.InstanceId,
			}, spec.LogOutput)
			if err != nil {
				return nil, err
			}
			return func(context.Context) error { return logger.Close() }, nil
		},
	})
	if cfg.Postgres != nil {
		plan = append(plan, Initializer{
			Name: "deer.postgres",
			Open: func(ctx context.Context) (Cleanup, error) {
				var err error
				database, err = postgres.New(ctx, cfg.Postgres, cfg.Name+"/"+cfg.InstanceId)
				if err != nil {
					return nil, err
				}
				return func(context.Context) error { return database.Close() }, nil
			},
		})
	}
	if cfg.Redis != nil {
		plan = append(plan, Initializer{
			Name: "deer.redis",
			Open: func(ctx context.Context) (Cleanup, error) {
				var err error
				cache, err = redis.New(ctx, cfg.Redis, cfg.Name+"/"+cfg.InstanceId)
				if err != nil {
					return nil, err
				}
				return func(context.Context) error { return cache.Close() }, nil
			},
		})
	}
	plan = append(plan, spec.Resources...)
	release, err := initializeResources(startup, plan, cfg.ShutdownTimeout.AsDuration())
	if err != nil {
		return nil, nil, err
	}
	d := &Deer{
		cfg:         cfg,
		release:     release,
		readySignal: make(chan struct{}),
		stopSignal:  make(chan struct{}),
		done:        make(chan struct{}),
		logger:      logger,
		database:    database,
		cache:       cache,
	}
	if cfg.Grpc != nil {
		if err := d.addGRPC(startup, spec.RegisterGRPC); err != nil {
			return nil, nil, errors.Join(err, d.cleanup(ctx))
		}
	}
	if err := startup.Err(); err != nil {
		return nil, nil, errors.Join(err, d.cleanup(ctx))
	}
	return d, d.cleanup, nil
}

func (d *Deer) addGRPC(ctx context.Context, register func(context.Context, *Deer, grpc.ServiceRegistrar) error) error {
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
		kratosgrpc.Timeout(d.cfg.Grpc.RequestTimeout.AsDuration()),
		kratosgrpc.DisableReflection(),
		kratosgrpc.Options(grpc.WaitForHandlers(true)),
		kratosgrpc.StreamInterceptor(streamDeadlineInterceptor(d.cfg.Grpc.RequestTimeout.AsDuration())),
	)
	d.servers = append(d.servers, newManagedTransport(server, accepting, "grpc", d.requestStop))
	d.grpcAddress = listener.Addr().String()
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
	servers := make([]transport.Server, len(d.servers))
	for index, server := range d.servers {
		servers[index] = server
	}
	app := kratos.New(
		kratos.Context(owned),
		kratos.Name(d.cfg.Name),
		kratos.ID(d.cfg.InstanceId),
		kratos.Version(d.cfg.Version),
		kratos.Server(servers...),
		kratos.StopTimeout(d.cfg.ShutdownTimeout.AsDuration()),
		kratos.AfterStart(d.markReady),
		kratos.BeforeStop(func(context.Context) error { d.requestStop(); return nil }),
	)
	// Run owns this single supervisor and joins it before releasing resources.
	completed := make(chan error, 1)
	go func() { completed <- app.Run() }()
	var runErr error
	appDone := false
	select {
	case runErr = <-completed:
		appDone = true
	case <-ctx.Done():
		runErr = ctx.Err()
	case <-d.stopSignal:
	}
	d.requestStop()
	stopErr := app.Stop()
	if stopErr != nil {
		cancel() // A failed stop hook must not leave the supervisor running.
	}
	if !appDone {
		runErr = errors.Join(runErr, <-completed)
	}
	shutdown, shutdownCancel := context.WithTimeout(context.WithoutCancel(ctx), d.cfg.ShutdownTimeout.AsDuration())
	defer shutdownCancel()
	for _, server := range d.servers {
		stopErr = errors.Join(stopErr, server.Stop(shutdown))
	}
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
		var stopErr error
		for _, server := range d.servers {
			stopErr = errors.Join(stopErr, server.Stop(shutdown))
		}
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
	d.closeErr = errors.Join(stopErr, d.release(ctx))
	close(d.done)
}
