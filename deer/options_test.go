package deer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"

	"github.com/hmlax/muskdeer/deer/logging"
)

func TestOptionsRejectNilBeforeAcquiringResources(t *testing.T) {
	t.Parallel()
	for name, option := range map[string]Option{
		"option":      nil,
		"logger":      WithLogger(nil),
		"database":    WithDB(nil),
		"redis":       WithRedis(nil),
		"jetstream":   WithJetStream(nil),
		"discovery":   WithDiscovery(nil),
		"grpcClients": WithGRPCClients(nil),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var acquired atomic.Int64
			owner, cleanup, err := NewDeer(t.Context(), serviceConfig("nil-option"), ServiceSpec{
				Resources: []Initializer{
					{
						Name: "must-not-open",
						Open: func(context.Context) (Cleanup, error) {
							acquired.Add(1)
							return func(context.Context) error { return nil }, nil
						},
					},
				},
			}, option)
			if err == nil || owner != nil || cleanup != nil || acquired.Load() != 0 {
				t.Fatalf("nil %s option = (%v, %v), resources %d; want failure before acquisition", name, owner, err, acquired.Load())
			}
		})
	}
}

func TestInjectedLoggerSurvivesRollbackAndNormalShutdown(t *testing.T) {
	t.Parallel()
	cfg := serviceConfig("borrowed-logger")
	cfg.Logging = serviceLogConfig(t)
	logger, err := logging.New(t.Context(), cfg.Logging, logging.Identity{
		Service:  cfg.Name,
		Instance: cfg.InstanceId,
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Error(err)
		}
	})
	failure := errors.New("registration refused")
	_, _, err = NewDeer(t.Context(), cfg, ServiceSpec{
		RegisterGRPC: func(_ context.Context, owner *Deer, _ grpc.ServiceRegistrar) error {
			if owner.Logger() != logger {
				t.Error("registration received a replacement logger")
			}
			return failure
		},
	}, WithLogger(logger))
	if !errors.Is(err, failure) {
		t.Fatalf("constructor rollback = %v; want registration failure", err)
	}
	if err := logger.LogAttrs(t.Context(), slog.LevelInfo, "after rollback"); err != nil {
		t.Fatalf("rollback closed borrowed logger: %v", err)
	}
	owner, cleanup, err := NewDeer(t.Context(), cfg, ServiceSpec{}, WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}
	result := runDeer(t, owner)
	if owner.app == nil || owner.app.Name() != cfg.Name || owner.app.ID() != cfg.InstanceId {
		t.Fatal("Deer did not retain its running Kratos application")
	}
	if err := cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if err := logger.LogAttrs(t.Context(), slog.LevelInfo, "after shutdown"); err != nil {
		t.Fatalf("shutdown closed borrowed logger: %v", err)
	}
}

func TestDeerApiAcceptsBorrowedDependencies(t *testing.T) {
	t.Parallel()
	cfg := apiConfig("borrowed-api-logger")
	logger, err := logging.New(t.Context(), nil, logging.Identity{
		Service:  cfg.Name,
		Instance: cfg.InstanceId,
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Error(err)
		}
	})
	owner, cleanup, err := NewDeerApi(t.Context(), cfg, APISpec{}, WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}
	if owner.Logger() != logger {
		t.Fatal("API did not reuse injected logger")
	}
	result := runDeer(t, owner.Deer)
	if err := cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if err := logger.LogAttrs(t.Context(), slog.LevelInfo, "after API cleanup"); err != nil {
		t.Fatal(err)
	}
}
