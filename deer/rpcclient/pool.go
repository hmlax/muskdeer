package rpcclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/hmlax/muskdeer/deer/nacos"
	"github.com/hmlax/muskdeer/deer/tracing"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

const (
	messageTransportNotReady = "RPC transport did not become ready"
	discoveryInterval        = time.Second
	discoveryMinimumInterval = 250 * time.Millisecond
	membershipMaximumAge     = 5 * time.Second
	maxDiscoveryInflight     = 4
)

// All route fields are guarded by Factory.mu. Each allowed service owns at most
// one refresh loop; caller cancellation never owns that loop's lifetime.
type serviceRoute struct {
	instances   []nacos.Instance
	failure     error
	checkedAt   time.Time
	initialized bool
	updated     chan struct{}
	wake        chan struct{}
	cancel      context.CancelFunc
}

type endpointKey struct {
	service string
	id      string
	version string
	address string
}

// The entry lock only guards construction (grpc.NewClient performs no I/O) and
// retirement. Dialing and RPCs never hold this lock or the factory lock.
type pooledConnection struct {
	mu      sync.Mutex
	conn    *grpc.ClientConn
	retired bool
}

func (f *Factory) transport(ctx context.Context, target Target) (*grpc.ClientConn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return nil, ErrClosed
	}
	route := f.routes[target.Service]
	if route == nil {
		owner, cancel := context.WithCancel(context.Background())
		route = &serviceRoute{
			updated: make(chan struct{}),
			wake:    make(chan struct{}, 1),
			cancel:  cancel,
		}
		f.routes[target.Service] = route
		f.wait.Add(1)
		go f.watchService(owner, target.Service, route)
	}
	initialized, updated := route.initialized, route.updated
	f.mu.Unlock()
	if !initialized {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.closing:
			return nil, ErrClosed
		case <-updated:
		}
	}
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return nil, ErrClosed
	}
	if route.failure != nil {
		err := route.failure
		f.mu.Unlock()
		return nil, err
	}
	if !route.initialized || time.Since(route.checkedAt) >= membershipMaximumAge {
		f.mu.Unlock()
		f.requestRefresh(target.Service)
		return nil, status.Error(codes.Unavailable, missingProvider)
	}
	instance, err := f.selectInstance(target, route.instances)
	if err != nil {
		f.mu.Unlock()
		return nil, err
	}
	key := endpointKey{
		service: target.Service,
		id:      instance.ID,
		version: instance.Version,
		address: instance.Address,
	}
	entry := f.connections[key]
	if entry == nil {
		entry = &pooledConnection{}
		f.connections[key] = entry
	}
	f.mu.Unlock()
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.retired {
		return nil, status.Error(codes.Unavailable, missingProvider)
	}
	if entry.conn == nil {
		dialer := &net.Dialer{Timeout: f.cfg.ConnectTimeout.AsDuration()}
		retry := backoff.DefaultConfig
		retry.BaseDelay = 100 * time.Millisecond
		retry.MaxDelay = time.Second
		entry.conn, err = grpc.NewClient("passthrough:///"+key.address,
			grpc.WithTransportCredentials(credentials.NewTLS(f.security[key.service])),
			grpc.WithNoProxy(), grpc.WithDisableRetry(), grpc.WithDisableServiceConfig(),
			grpc.WithIdleTimeout(time.Minute),
			grpc.WithConnectParams(grpc.ConnectParams{
				Backoff:           retry,
				MinConnectTimeout: f.cfg.ConnectTimeout.AsDuration(),
			}),
			grpc.WithStatsHandler(tracing.ClientHandler(f.provider)),
			grpc.WithContextDialer(func(ctx context.Context, address string) (net.Conn, error) {
				return dialer.DialContext(ctx, "tcp", address)
			}),
		)
		if err != nil {
			return nil, err
		}
	}
	return entry.conn, nil
}

func (f *Factory) waitReady(ctx context.Context, target Target, conn *grpc.ClientConn) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	connect, cancel := context.WithTimeout(ctx, f.cfg.ConnectTimeout.AsDuration())
	defer cancel()
	conn.Connect()
	for state := conn.GetState(); state != connectivity.Ready; state = conn.GetState() {
		if state == connectivity.TransientFailure || state == connectivity.Shutdown {
			f.requestRefresh(target.Service)
		}
		if state == connectivity.Shutdown || !conn.WaitForStateChange(connect, state) {
			return contextualError(connect, status.Error(codes.Unavailable, messageTransportNotReady))
		}
	}
	return nil
}

func (f *Factory) requestRefresh(service string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	if route := f.routes[service]; route != nil {
		select {
		case route.wake <- struct{}{}:
		default:
		}
	}
}

func (f *Factory) watchService(ctx context.Context, service string, route *serviceRoute) {
	defer f.wait.Done()
	for ctx.Err() == nil {
		started := time.Now()
		f.refreshService(ctx, service, route)
		timer := time.NewTimer(discoveryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		case <-route.wake:
			timer.Stop()
		}
		// Coalesce disconnect notifications: many waiting calls never create an
		// unbounded discovery storm or another business RPC attempt.
		if left := discoveryMinimumInterval - time.Since(started); left > 0 {
			timer := time.NewTimer(left)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
}

func (f *Factory) refreshService(ctx context.Context, service string, route *serviceRoute) {
	call, cancel := context.WithTimeout(ctx, min(f.cfg.RequestTimeout.AsDuration(), 2*time.Second))
	defer cancel()
	select {
	case f.lookupSlots <- struct{}{}:
	case <-call.Done():
		f.publishRoute(service, route, nil, call.Err())
		return
	}
	instances, err := f.discovery.GetService(call, service)
	<-f.lookupSlots
	if err == nil {
		err = validateInstances(service, instances)
	}
	if err != nil {
		err = fmt.Errorf("discover RPC target: %w", err)
	}
	f.publishRoute(service, route, instances, err)
}

func (f *Factory) publishRoute(service string, route *serviceRoute, instances []nacos.Instance, failure error) {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return
	}
	if failure != nil {
		instances = nil
	}
	route.instances = append([]nacos.Instance(nil), instances...)
	route.failure = failure
	route.checkedAt = time.Now()
	route.initialized = true
	current := make(map[endpointKey]struct{}, len(instances))
	for _, instance := range instances {
		current[endpointKey{
			service: service,
			id:      instance.ID,
			version: instance.Version,
			address: instance.Address,
		}] = struct{}{}
	}
	var retired []*pooledConnection
	for key, entry := range f.connections {
		if key.service != service {
			continue
		}
		if _, valid := current[key]; !valid {
			delete(f.connections, key)
			retired = append(retired, entry)
		}
	}
	close(route.updated)
	route.updated = make(chan struct{})
	f.mu.Unlock()
	var closeErr error
	for _, entry := range retired {
		closeErr = errors.Join(closeErr, entry.close())
	}
	if closeErr != nil {
		f.mu.Lock()
		f.retirementErr = errors.Join(f.retirementErr, closeErr)
		f.mu.Unlock()
	}
}

func (c *pooledConnection) close() error {
	c.mu.Lock()
	if c.retired {
		c.mu.Unlock()
		return nil
	}
	c.retired = true
	conn := c.conn
	c.mu.Unlock()
	if conn != nil {
		return conn.Close()
	}
	return nil
}
