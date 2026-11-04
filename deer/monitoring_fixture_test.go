package deer

import (
	"context"
	"errors"

	"github.com/cloudwego/hertz/pkg/app/server"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"google.golang.org/protobuf/proto"
)

type monitoringFixtureSpec struct {
	Service ServiceSpec
	Routes  func(context.Context, *Deer, *server.Hertz) error
}

type monitoringFixture struct {
	*Deer
	address string
}

func (m *monitoringFixture) HTTPAddress() string { return m.address }

func newMonitoringFixture(ctx context.Context, cfg *configv1.ServiceConfig, spec monitoringFixtureSpec, options ...Option) (*monitoringFixture, Cleanup, error) {
	cfg = proto.CloneOf(cfg)
	listener := cfg.Monitoring
	cfg.Monitoring = nil
	owner, cleanup, err := NewDeer(ctx, cfg, spec.Service, options...)
	if err != nil {
		return nil, nil, err
	}
	hertz, address, err := owner.addHertz(ctx, listener, "monitoring HTTP")
	if err == nil && spec.Routes != nil {
		err = spec.Routes(ctx, owner, hertz)
	}
	if err != nil {
		return nil, nil, errors.Join(err, cleanup(ctx))
	}
	return &monitoringFixture{
		Deer:    owner,
		address: address,
	}, cleanup, nil
}
