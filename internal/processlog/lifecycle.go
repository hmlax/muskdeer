package processlog

import (
	"context"
	"errors"
	"log/slog"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/logging"
)

// Lifecycle binds SDK logging and its stopped record to one Deer construction.
// Its methods run synchronously during construction and owned resource cleanup.
type Lifecycle struct {
	logger *logging.Logger
}

// NewLifecycle provides process-local logging state without acquiring resources.
func NewLifecycle() *Lifecycle { return &Lifecycle{} }

// Install installs the sole application's logging before SDK resources open.
func (l *Lifecycle) Install(ctx context.Context, owned *logging.Logger) (deer.Cleanup, error) {
	cleanup, err := Install(ctx, owned)
	if err != nil {
		return nil, err
	}
	l.logger = owned
	return cleanup, nil
}

// Open requires installed logging and returns a post-transport cleanup record.
func (l *Lifecycle) Open(context.Context) (deer.Cleanup, error) {
	if l.logger == nil {
		return nil, errors.New("process lifecycle requires installed logging")
	}
	return func(ctx context.Context) error {
		return l.logger.LogAttrs(ctx, slog.LevelInfo, "service.transports_stopped")
	}, nil
}
