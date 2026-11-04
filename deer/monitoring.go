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
	"github.com/hmlax/muskdeer/deer/requestid"
)

type hertzTransport struct {
	server  *server.Hertz
	network network.Transporter
}

var _ transport.Server = (*hertzTransport)(nil)

func (d *Deer) addHertz(ctx context.Context, listenerConfig *configv1.Listener, label string) (*server.Hertz, string, error) {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", listenerConfig.Address)
	if err != nil {
		return nil, "", fmt.Errorf("listen %s: %w", label, err)
	}
	accepting := &acceptingListener{
		Listener: listener,
		ready:    make(chan struct{}),
	}
	timeout := listenerConfig.RequestTimeout.AsDuration()
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
	hertz.Use(func(ctx context.Context, request *app.RequestContext) {
		started := time.Now()
		completed := false
		defer func() {
			if request.FullPath() == "/metrics" {
				return // Scraping never changes the samples being scraped.
			}
			code := http.StatusInternalServerError
			if completed {
				code = request.Response.StatusCode()
			}
			d.metrics.ObserveProbe(code, time.Since(started))
		}()
		// Supply Date per response without Hertz's process-global ticker.
		request.Response.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
		bounded, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		var candidate string
		values := request.Request.Header.PeekAll(requestid.Header)
		if len(values) == 1 && len(values[0]) <= 64 {
			candidate = string(values[0])
		}
		id, err := requestid.New(bounded, candidate)
		if err != nil {
			request.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		request.Response.Header.Set(requestid.Header, id.String())
		request.Next(requestid.Bind(bounded, id))
		completed = true
	})
	return hertz, listener.Addr().String(), nil
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
