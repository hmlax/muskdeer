package deer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/hmlax/muskdeer/deer/logging"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

func TestDeerLoggerRollbackReleasesFileOwnership(t *testing.T) {
	t.Parallel()
	cfg := serviceConfig("logging-rollback")
	cfg.Logging = serviceLogConfig(t)
	failure := errors.New("dependency unavailable")
	_, _, err := NewDeer(t.Context(), cfg, ServiceSpec{
		Resources: []Initializer{
			{
				Name: "failing",
				Open: func(context.Context) (Cleanup, error) { return nil, failure },
			},
		},
	})
	if !errors.Is(err, failure) {
		t.Fatalf("NewDeer error = %v; want original dependency error", err)
	}
	logger, err := logging.New(t.Context(), cfg.Logging, logging.Identity{
		Service:  cfg.Name,
		Instance: cfg.InstanceId,
	}, nil)
	if err != nil {
		t.Fatalf("failed constructor retained its log file lock: %v", err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDeerReleasesLoggerAfterOtherDependencies(t *testing.T) {
	t.Parallel()
	cfg := serviceConfig("logging-order")
	cfg.Logging = serviceLogConfig(t)
	var service *Deer
	service, cleanup, err := NewDeer(t.Context(), cfg, ServiceSpec{
		Resources: []Initializer{
			{
				Name: "dependency",
				Open: func(context.Context) (Cleanup, error) {
					return func(ctx context.Context) error {
						return service.Logger().LogAttrs(ctx, slog.LevelInfo, "dependency closed")
					}, nil
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := runDeer(t, service)
	if err := cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(service.Logger().Filename())
	if err != nil || !bytes.Contains(data, []byte("dependency closed")) {
		t.Fatalf("cleanup log = %q, %v", data, err)
	}
	if err := service.Logger().LogAttrs(t.Context(), slog.LevelInfo, "after cleanup"); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("logger after service cleanup = %v", err)
	}
}

func TestDeerApiOwnsConfiguredLogger(t *testing.T) {
	t.Parallel()
	cfg := apiConfig("api-logging")
	cfg.Logging = serviceLogConfig(t)
	api, cleanup, err := NewDeerApi(t.Context(), cfg, APISpec{})
	if err != nil {
		t.Fatal(err)
	}
	if err := api.Logger().LogAttrs(t.Context(), slog.LevelInfo, "api assembled"); err != nil {
		t.Fatal(err)
	}
	if err := cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(api.Logger().Filename())
	if err != nil || bytes.Count(data, []byte("\n")) != 1 {
		t.Fatalf("API shared log = %q, %v", data, err)
	}
}

func TestInvalidLoggingDoesNotOpenBusinessDependencies(t *testing.T) {
	t.Parallel()
	cfg := serviceConfig("invalid-log")
	cfg.Logging = &configv1.Logging{}
	var count atomic.Int64
	_, _, err := NewDeer(t.Context(), cfg, ServiceSpec{
		Resources: []Initializer{
			{
				Name: "must not open",
				Open: func(context.Context) (Cleanup, error) {
					count.Add(1)
					return func(context.Context) error { return nil }, nil
				},
			},
		},
	})
	if err == nil || count.Load() != 0 {
		t.Fatalf("invalid logging = %v, dependencies opened %d", err, count.Load())
	}
}

func serviceLogConfig(t *testing.T) *configv1.Logging {
	t.Helper()
	return &configv1.Logging{
		Level: logging.LevelInfo,
		File: &configv1.LogFile{
			Filename:   filepath.Join(t.TempDir(), "service.log"),
			MaxSizeMb:  1,
			MaxBackups: 2,
			MaxAgeDays: 1,
			Compress:   true,
		},
	}
}
