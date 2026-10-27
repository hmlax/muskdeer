package deer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"

	"github.com/hmlax/muskdeer/deer/logging"
)

func TestProcessLoggingRollbackKeepsLoggerUntilBridgeRestored(t *testing.T) {
	for _, api := range []bool{false, true} {
		t.Run(map[bool]string{false: "Deer", true: "DeerApi"}[api], func(t *testing.T) {
			t.Parallel()
			failure := errors.New("dependency unavailable")
			var observed []string
			var logger *logging.Logger
			spec := ServiceSpec{
				LogOutput: io.Discard,
				SetupProcessLogging: func(_ context.Context, owned *logging.Logger) (Cleanup, error) {
					logger = owned
					observed = append(observed, "bridge.open")
					return func(ctx context.Context) error {
						observed = append(observed, "bridge.close")
						return owned.LogAttrs(ctx, slog.LevelInfo, "bridge restored")
					}, nil
				},
				Resources: []Initializer{
					{
						Name: "failing.dependency",
						Open: func(context.Context) (Cleanup, error) {
							observed = append(observed, "dependency.open")
							return func(context.Context) error {
								observed = append(observed, "dependency.close")
								return nil
							}, failure
						},
					},
				},
			}
			var err error
			if api {
				_, _, err = NewDeerApi(t.Context(), apiConfig("bridge-rollback"), APISpec{Service: spec})
			} else {
				_, _, err = NewDeer(t.Context(), serviceConfig("bridge-rollback"), spec)
			}
			if !errors.Is(err, failure) || !slices.Equal(observed, []string{"bridge.open", "dependency.open", "dependency.close", "bridge.close"}) {
				t.Fatalf("rollback = %v, %v", observed, err)
			}
			if err := logger.LogAttrs(t.Context(), slog.LevelInfo, "after rollback"); !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("owned logger remained open: %v", err)
			}
		})
	}
}
