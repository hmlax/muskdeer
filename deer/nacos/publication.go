package nacos

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"
)

// ErrNotPublished reports a publication that has not completed registration.
var ErrNotPublished = errors.New("nacos instance is not published")

// Stay below Nacos's 15-second unhealthy threshold. A failed attempt never
// extends the deadline established by the last successful heartbeat.
const heartbeatFreshness = 2 * maximumBeat

// Publication owns one ephemeral registration and its heartbeat. Construct it
// before startup, Start once after listeners/dependencies are ready, and Stop
// before closing Client. Observe Failures to stop a service that loses discovery.
type Publication struct {
	client      *Client
	record      Registration
	interval    time.Duration
	mu          sync.Mutex
	started     bool
	registered  bool
	closed      bool
	cancel      context.CancelFunc
	lastSuccess time.Time
	err         error
	initialized chan struct{}
	done        chan struct{}
	failures    chan error
	once        sync.Once
	stopErr     error
}

// NewPublication validates a copied registration without network I/O.
func NewPublication(client *Client, record Registration, interval time.Duration) (*Publication, error) {
	if client == nil {
		return nil, errors.New("nacos publication requires an owned client")
	}
	if err := validateRegistration(record); err != nil {
		return nil, err
	}
	if err := validateInterval(interval); err != nil {
		return nil, err
	}
	return &Publication{
		client:      client,
		record:      record,
		interval:    interval,
		initialized: make(chan struct{}),
		done:        make(chan struct{}),
		failures:    make(chan error, 1),
	}, nil
}

// Start registers within ctx and then transfers heartbeat ownership to Stop.
// Even failed/ambiguous Start must be followed by Stop to attempt deregistration.
// The startup context is not retained by the background loop.
func (p *Publication) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("nacos publication startup context is required")
	}
	p.mu.Lock()
	if p.started || p.closed {
		p.mu.Unlock()
		return errors.New("nacos publication can only start once before stop")
	}
	p.started = true
	startup, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	p.mu.Unlock()
	defer close(p.initialized)
	err := p.client.Register(startup, p.record)
	err = errors.Join(err, startup.Err())
	cancel()
	p.mu.Lock()
	if p.closed {
		err = errors.Join(err, ErrClosed)
	}
	if err != nil {
		p.err = err
		p.mu.Unlock()
		close(p.done)
		return err
	}
	// The returned object, rather than the startup request, owns this loop.
	owned, stop := context.WithCancel(context.Background())
	p.cancel = stop
	p.registered = true
	p.lastSuccess = time.Now()
	p.mu.Unlock()
	go p.maintain(owned)
	return nil
}

// Failures delivers at most one terminal heartbeat failure. It remains silent on
// normal Stop; the bounded notification is not a message stream to range over.
func (p *Publication) Failures() <-chan error { return p.failures }

// Health checks registration and heartbeat freshness below the pinned server's
// default 15-second unhealthy threshold. Nacos membership is not trade authority.
func (p *Publication) Health() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrClosed
	}
	if p.err != nil {
		return p.err
	}
	if !p.registered {
		return ErrNotPublished
	}
	if time.Since(p.lastSuccess) >= heartbeatFreshness {
		return errors.New("nacos heartbeat freshness expired")
	}
	return nil
}

// Stop prevents new beats, cancels/joins startup and heartbeat work, then attempts
// deregistration even after a failed Start. An unused publication does no I/O.
func (p *Publication) Stop(ctx context.Context) error {
	if ctx == nil {
		return errors.New("nacos publication shutdown context is required")
	}
	p.once.Do(func() {
		p.mu.Lock()
		p.closed = true
		started := p.started
		cancel := p.cancel
		if !started {
			close(p.initialized)
			close(p.done)
		}
		p.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		<-p.initialized
		<-p.done
		if started {
			p.stopErr = p.client.Deregister(ctx, p.record)
		}
	})
	return p.stopErr
}

func (p *Publication) maintain(ctx context.Context) {
	defer close(p.done)
	timer := time.NewTimer(p.interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		p.mu.Lock()
		until := p.lastSuccess.Add(heartbeatFreshness)
		p.mu.Unlock()
		attempt, cancel := context.WithDeadline(ctx, until)
		recommended, err := p.client.Beat(attempt, p.record)
		cancel()
		p.mu.Lock()
		if p.closed || ctx.Err() != nil {
			p.mu.Unlock()
			return
		}
		if err != nil {
			if retryHeartbeat(err) && time.Now().Before(until) {
				p.mu.Unlock()
				timer.Reset(min(p.interval, time.Until(until)))
				continue
			}
			p.err = err
			p.mu.Unlock()
			p.failures <- err
			return
		}
		p.lastSuccess = time.Now()
		p.mu.Unlock()
		timer.Reset(min(p.interval, recommended))
	}
}

// Only this publication owns heartbeat recovery. Authentication, a missing
// registration and malformed protocol replies remain terminal; recovery never
// issues a new registration or retries financial operations.
func retryHeartbeat(err error) bool {
	var failure *APIError
	if errors.As(err, &failure) {
		if failure.HTTPStatus != 0 {
			switch failure.HTTPStatus {
			case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
				return true
			default:
				return false
			}
		}
		if failure.Code != 0 {
			return false
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var network net.Error
	return errors.As(err, &network) && network.Timeout()
}

func validateInterval(interval time.Duration) error {
	if interval < minimumBeat || interval > maximumBeat || interval%time.Millisecond != 0 {
		return errors.New("nacos heartbeat interval must be whole milliseconds in 50..5000")
	}
	return nil
}
