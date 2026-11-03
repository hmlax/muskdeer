package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/hmlax/muskdeer/deer"
)

// App owns one Deer and the cleanup of its complete dependency graph.
type App struct {
	owner   *deer.Deer
	cleanup deer.Cleanup
}

// New is the final fallible provider; Deer rolls back partial construction itself.
func New(ctx context.Context, cfg *Config, spec deer.ServiceSpec, options []deer.Option) (*App, error) {
	owner, cleanup, err := deer.NewDeer(ctx, cfg.service, spec, options...)
	if err != nil {
		return nil, err
	}
	return &App{
		owner:   owner,
		cleanup: cleanup,
	}, nil
}

// Run starts the owned service once and joins all workers before cleanup.
func (a *App) Run(ctx context.Context) (result error) {
	defer func() { result = errors.Join(result, a.Close(ctx)) }()
	if err := a.owner.Logger().LogAttrs(ctx, slog.LevelInfo, "core.initialized",
		slog.String("grpc_address", a.owner.GRPCAddress()),
		slog.String("monitoring_address", a.owner.MonitoringAddress())); err != nil {
		return err
	}
	return a.owner.Run(ctx)
}

// Close releases the graph, including when construction succeeded but Run is canceled.
// The Deer cleanup is idempotent and preserves cleanup errors.
func (a *App) Close(ctx context.Context) error { return a.cleanup(ctx) }
