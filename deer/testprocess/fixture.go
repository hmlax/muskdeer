// Package testprocess supports independently owned regression processes.
package testprocess

import (
	"context"
	"errors"
	"io"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/logging"
	"github.com/hmlax/muskdeer/deer/processlog"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

// Runtime describes work supplied by the owning service fixture.
type Runtime interface {
	Workers() []deer.Worker
	RegisterGRPC(grpc.ServiceRegistrar) error
}

// Build assembles one service using its already initialized Deer resources.
type Build func(context.Context, *configv1.ServiceConfig, *deer.Deer) (Runtime, error)

// RunFixture validates a service/config match, creates one Deer, and
// joins Run before returning. SDK logging is installed before dependencies or
// transports use SDK globals. Call only once in a fresh, sole service process.
func RunFixture(ctx context.Context, name string, cfg *configv1.ServiceConfig, output io.Writer, build Build) error {
	if ctx == nil || cfg == nil {
		return errors.New("service entry requires context and configuration")
	}
	cfg = proto.CloneOf(cfg)
	if cfg.AdminHttp != nil || cfg.Http != nil {
		return errors.New("module fixture does not support management HTTP")
	}
	if cfg.Name != name || cfg.Monitoring == nil || cfg.Grpc == nil || build == nil {
		return errors.New("service fixture requires a matching identity, gRPC and monitoring listeners and a typed builder")
	}
	var logger *logging.Logger
	spec := deer.ServiceSpec{
		LogOutput: output,
		SetupProcessLogging: func(ctx context.Context, owned *logging.Logger) (deer.Cleanup, error) {
			logger = owned
			return processlog.Install(ctx, owned)
		},
		Resources: []deer.Initializer{
			{
				Name: "service.lifecycle",
				Open: func(context.Context) (deer.Cleanup, error) {
					return func(ctx context.Context) error {
						return logger.LogAttrs(ctx, slog.LevelInfo, "service.transports_stopped")
					}, nil
				},
			},
		},
	}
	if cfg.GetBusiness().GetEnabled() {
		var runtime Runtime
		spec.Workers = func(ctx context.Context, owner *deer.Deer) ([]deer.Worker, error) {
			var err error
			runtime, err = build(ctx, cfg, owner)
			if err != nil {
				return nil, err
			}
			return runtime.Workers(), nil
		}
		spec.RegisterGRPC = func(_ context.Context, _ *deer.Deer, registrar grpc.ServiceRegistrar) error {
			return runtime.RegisterGRPC(registrar)
		}
	}
	owner, cleanup, err := deer.NewDeer(ctx, cfg, spec)
	if err != nil {
		return err
	}
	return runOwned(ctx, owner, cleanup)
}

func runOwned(ctx context.Context, owner *deer.Deer, cleanup deer.Cleanup) (result error) {
	defer func() { result = errors.Join(result, cleanup(ctx)) }()
	if err := owner.Logger().LogAttrs(ctx, slog.LevelInfo, "service.initialized",
		slog.String("grpc_address", owner.GRPCAddress()),
		slog.String("monitoring_address", owner.MonitoringAddress())); err != nil {
		return err
	}
	return owner.Run(ctx)
}
