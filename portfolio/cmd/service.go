package main

import (
	"context"
	"errors"
	"io"
	"log/slog"

	"google.golang.org/protobuf/proto"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/logging"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/internal/business"
	"github.com/hmlax/muskdeer/internal/processlog"
)

// runService owns the portfolio process, its dependencies and its work plan.
func runService(ctx context.Context, cfg *configv1.ServiceConfig, output io.Writer) (result error) {
	if ctx == nil || cfg == nil || cfg.Name != "portfolio" || cfg.Http != nil || cfg.AdminHttp != nil || cfg.Monitoring == nil || cfg.Grpc == nil ||
		cfg.GetBusiness().GetStrategy().GetManagementEnabled() || cfg.GetBusiness().GetStrategy().GetControlEnabled() {
		return errors.New("portfolio service requires its own gRPC configuration without administration")
	}
	cfg = proto.CloneOf(cfg)
	if err := business.ValidateConfig(cfg); err != nil {
		return err
	}
	var logger *logging.Logger
	spec := deer.ServiceSpec{
		LogOutput: output,
		SetupProcessLogging: func(ctx context.Context, owned *logging.Logger) (deer.Cleanup, error) {
			logger = owned
			return processlog.Install(ctx, owned)
		},
		Resources: []deer.Initializer{{
			Name: "portfolio.lifecycle",
			Open: func(context.Context) (deer.Cleanup, error) {
				return func(ctx context.Context) error {
					return logger.LogAttrs(ctx, slog.LevelInfo, "service.transports_stopped")
				}, nil
			},
		}},
	}
	if cfg.GetBusiness().GetEnabled() {
		spec.Workers = func(ctx context.Context, owner *deer.Deer) ([]deer.Worker, error) {
			runtime, err := business.NewPortfolio(ctx, cfg, owner)
			if err != nil {
				return nil, err
			}
			return runtime.Workers(), nil
		}
	}
	owner, cleanup, err := deer.NewDeer(ctx, cfg, spec)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, cleanup(ctx)) }()
	if err := owner.Logger().LogAttrs(ctx, slog.LevelInfo, "core.initialized",
		slog.String("grpc_address", owner.GRPCAddress()),
		slog.String("monitoring_address", owner.MonitoringAddress())); err != nil {
		return err
	}
	return owner.Run(ctx)
}
