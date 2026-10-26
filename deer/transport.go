package deer

import (
	"context"
	"errors"
	"net"
	"net/url"
	"sync"

	"github.com/go-kratos/kratos/v2/transport"
)

// acceptingListener distinguishes a reserved port from a started transport.
type acceptingListener struct {
	net.Listener
	once  sync.Once
	ready chan struct{}
}

type managedTransport struct {
	server    transport.Server
	listener  *acceptingListener
	endpoint  *url.URL
	done      chan struct{}
	mu        sync.Mutex
	started   bool
	stopped   bool
	startErr  error
	stopOnce  sync.Once
	stopErr   error
	onFailure func()
}

var (
	_ transport.Server     = (*managedTransport)(nil)
	_ transport.Endpointer = (*managedTransport)(nil)
)

func newManagedTransport(server transport.Server, listener *acceptingListener, scheme string, onFailure func()) *managedTransport {
	return &managedTransport{
		server:   server,
		listener: listener,
		endpoint: &url.URL{
			Scheme: scheme,
			Host:   listener.Addr().String(),
		},
		done:      make(chan struct{}),
		onFailure: onFailure,
	}
}

func (l *acceptingListener) Accept() (net.Conn, error) {
	l.once.Do(func() { close(l.ready) })
	return l.Listener.Accept()
}

func (s *managedTransport) Endpoint() (*url.URL, error) {
	endpoint := *s.endpoint
	return &endpoint, nil
}

func (s *managedTransport) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil
	}
	s.started = true
	s.mu.Unlock()
	err := s.server.Start(ctx)
	s.mu.Lock()
	if err == nil && !s.stopped {
		err = errors.New("transport stopped unexpectedly")
	}
	s.startErr = err
	failed := !s.stopped
	s.mu.Unlock()
	if failed {
		s.onFailure()
	}
	close(s.done)
	return err
}

func (s *managedTransport) Stop(ctx context.Context) error {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.stopped = true
		started := s.started
		if !started {
			close(s.done)
		}
		s.mu.Unlock()
		if started {
			// Stop may race Kratos's Start goroutine. Wait for actual transport
			// initialization, or for Start to fail, before asking it to drain.
			select {
			case <-s.listener.ready:
			case <-s.done:
			case <-ctx.Done():
				s.stopErr = ctx.Err()
				s.stopErr = errors.Join(s.stopErr, closeListener(s.listener))
			}
		}
		// A stopped accept loop can still have accepted requests. Always stop the
		// transport itself before releasing its dependencies.
		s.stopErr = errors.Join(s.stopErr, s.server.Stop(ctx), ctx.Err())
		s.stopErr = errors.Join(s.stopErr, closeListener(s.listener))
		<-s.done
	})
	return s.stopErr
}

func (s *managedTransport) waitReady(ctx context.Context) error {
	select {
	case <-s.listener.ready:
		return nil
	case <-s.done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return errors.Join(errors.New("transport stopped before readiness"), s.startErr)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func closeListener(listener net.Listener) error {
	err := listener.Close()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}
