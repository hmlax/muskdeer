package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/go-kratos/kratos/v2/registry"
	"github.com/google/uuid"
	"github.com/hmlax/muskdeer/deer/management/runtimeconfig"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
)

// Run owns listener goroutines, discovery registration and graceful shutdown.
func Run(ctx context.Context, boot *bootstrap.Context, servers []*HTTPServer) (result error) {
	if len(servers) == 0 {
		return fmt.Errorf("admin has no HTTP server")
	}
	ready := make(chan struct{}, len(servers))
	done := make(chan error, len(servers))
	received := 0
	reg := runtimeconfig.Registrar(boot)
	info := boot.GetAppInfo()
	instance := &registry.ServiceInstance{
		ID:      info.GetInstanceId(),
		Name:    info.GetAppId(),
		Version: info.GetVersion(),
	}
	if instance.ID == "" {
		instance.ID = uuid.NewString()
	}
	for _, srv := range servers {
		instance.Endpoints = append(instance.Endpoints, srv.endpoint.String())
	}
	registrationAttempted := false
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if registrationAttempted {
			result = errors.Join(result, reg.Deregister(shutdown, instance))
		}
		for _, srv := range servers {
			srv.cancel()
		}
		for _, srv := range servers {
			if srv.hertz.IsRunning() {
				result = errors.Join(result, srv.Stop(shutdown))
			}
			if err := srv.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				result = errors.Join(result, err)
			}
		}
		for received < len(servers) {
			select {
			case err := <-done:
				received++
				result = errors.Join(result, err)
			case <-shutdown.Done():
				result = errors.Join(result, shutdown.Err())
				return
			}
		}
	}()
	for _, srv := range servers {
		srv.hertz.OnRun = append(srv.hertz.OnRun, func(context.Context) error { ready <- struct{}{}; return nil })
		go func() { done <- srv.Start(ctx) }()
	}
	for started := 0; started < len(servers); {
		select {
		case <-ctx.Done():
			return nil
		case err := <-done:
			received++
			if err == nil {
				err = fmt.Errorf("HTTP server stopped during startup")
			}
			return err
		case <-ready:
			started++
		}
	}
	if reg != nil {
		// Register may publish one endpoint before another endpoint fails.
		registrationAttempted = true
		startup, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := reg.Register(startup, instance)
		cancel()
		if err != nil {
			return fmt.Errorf("register admin HTTP service: %w", err)
		}
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-done:
		received++
		if err == nil {
			err = fmt.Errorf("HTTP server stopped unexpectedly")
		}
		return err
	}
}
