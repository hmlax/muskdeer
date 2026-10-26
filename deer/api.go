package deer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/network"
	"github.com/cloudwego/hertz/pkg/network/standard"
	"github.com/go-kratos/kratos/v2/transport"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

// APISpec extends the same service assembly with synchronous Hertz route setup.
// Routes may register middleware and handlers; it must not run the server or add
// independent lifecycle hooks. Handlers must observe their request context.
type APISpec struct {
	Service ServiceSpec
	Routes  func(*server.Hertz) error
}

// DeerApi composes exactly one Deer and adds its Hertz HTTP transport.
//
//revive:disable-next-line:exported,var-naming // User-mandated public name; see docs/DEER.md.
type DeerApi struct {
	*Deer
	httpAddress string
}

type hertzTransport struct {
	server  *server.Hertz
	network network.Transporter
}

var _ transport.Server = (*hertzTransport)(nil)

// NewDeerApi acquires shared dependencies once and registers both transports in
// the same Kratos lifecycle. HTTP failure also releases the already built Deer.
//
//revive:disable-next-line:var-naming // User-mandated constructor spelling; see docs/DEER.md.
func NewDeerApi(ctx context.Context, cfg *configv1.ServiceConfig, spec APISpec) (*DeerApi, Cleanup, error) {
	if ctx == nil || cfg == nil {
		return nil, nil, errors.New("construction context and configuration are required")
	}
	startup, cancel := context.WithTimeout(ctx, cfg.GetStartupTimeout().AsDuration())
	defer cancel()
	shared, cleanup, err := newDeer(startup, cfg, spec.Service, true)
	if err != nil {
		return nil, nil, err
	}
	api := &DeerApi{Deer: shared}
	if err := api.addHTTP(startup, spec.Routes); err != nil {
		return nil, nil, errors.Join(err, cleanup(ctx))
	}
	if err := startup.Err(); err != nil {
		return nil, nil, errors.Join(err, cleanup(ctx))
	}
	return api, cleanup, nil
}

// HTTPAddress returns the actual reserved HTTP listener address.
func (d *DeerApi) HTTPAddress() string { return d.httpAddress }

func (d *DeerApi) addHTTP(ctx context.Context, routes func(*server.Hertz) error) error {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", d.cfg.Http.Address)
	if err != nil {
		return fmt.Errorf("listen HTTP: %w", err)
	}
	accepting := &acceptingListener{
		Listener: listener,
		ready:    make(chan struct{}),
	}
	timeout := d.cfg.Http.RequestTimeout.AsDuration()
	owned := &hertzTransport{}
	hertz := server.New(
		server.WithListener(accepting),
		server.WithTransport(func(options *config.Options) network.Transporter {
			owned.network = standard.NewTransporter(options)
			return owned.network
		}),
		server.WithReadTimeout(timeout),
		server.WithWriteTimeout(timeout),
		server.WithIdleTimeout(timeout),
		server.WithExitWaitTime(d.cfg.ShutdownTimeout.AsDuration()),
		server.WithDisableDefaultDate(true),
	)
	owned.server = hertz
	d.servers = append(d.servers, newManagedTransport(owned, accepting, "http", d.requestStop))
	d.httpAddress = listener.Addr().String()
	hertz.Use(func(ctx context.Context, request *app.RequestContext) {
		// Supply Date per response without Hertz's process-global ticker.
		request.Response.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
		bounded, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		request.Next(bounded)
	})
	if routes != nil {
		if err := routes(hertz); err != nil {
			return fmt.Errorf("register HTTP routes: %w", err)
		}
	}
	return nil
}

func (s *hertzTransport) Start(context.Context) error { return s.server.Run() }

func (s *hertzTransport) Stop(ctx context.Context) error {
	var err error
	if s.server.IsRunning() {
		err = s.server.Shutdown(ctx)
	}
	// The accept loop can already have exited after listener failure. Drain its
	// accepted connections even when Engine.Shutdown no longer accepts the state.
	err = errors.Join(err, s.network.Shutdown(ctx))
	// Hertz suppresses a transport error equal to ctx.Err; preserve deadline failure.
	return errors.Join(err, ctx.Err())
}
