// Package testapp verifies process assembly without running business workers.
package testapp

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	kratoslog "github.com/go-kratos/kratos/v2/log"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/logging"
	configv1 "github.com/hmlax/muskdeer/deer/proto/common/config/v1"
)

// Application owns resources even when startup is canceled before Run.
type Application interface {
	Run(context.Context) error
	Close(context.Context) error
}

// Build constructs exactly one process application with its generated injector.
type Build func(context.Context, *configv1.ServiceConfig, io.Writer, []deer.Option) (Application, error)

// AssertLifecycle checks late startup rollback, SDK logger restoration and closing
// a successfully constructed application before Run. It must not run in parallel.
func AssertLifecycle(t *testing.T, name string, build Build) {
	t.Helper()
	cfg, err := deer.LoadSplitConfig(t.Context(), filepath.Join("..", "..", "configs", "local", name))
	if err != nil {
		t.Fatal(err)
	}
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := occupied.Close(); err != nil {
			t.Error(err)
		}
	})
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Grpc.Address = probe.Addr().String()
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	cfg.Monitoring.Address = occupied.Addr().String()
	kratosBefore, hertzBefore := kratoslog.GetLogger(), hlog.DefaultLogger()
	var output bytes.Buffer
	application, err := build(t.Context(), cfg, &output, nil)
	if err == nil || application != nil {
		t.Fatal("injector did not reject an occupied monitoring listener")
	}
	assertLoggers(t, kratosBefore, hertzBefore)
	probe, err = net.Listen("tcp", cfg.Grpc.Address)
	if err != nil {
		t.Fatalf("failed construction leaked its gRPC listener: %v", err)
	}
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	cfg.Monitoring.Address = "127.0.0.1:0"
	application, err = build(t.Context(), cfg, &output, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := application.Close(t.Context()); err != nil {
			t.Error(err)
		}
	})
	if err := application.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertLoggers(t, kratosBefore, hertzBefore)
	if !bytes.Contains(output.Bytes(), []byte(`"msg":"service.transports_stopped"`)) {
		t.Fatal("owned lifecycle did not emit its stopped record")
	}
	stoppedBefore := bytes.Count(output.Bytes(), []byte(`"msg":"service.transports_stopped"`))
	logger, err := logging.New(t.Context(), nil, logging.Identity{
		Service:  cfg.Name,
		Instance: cfg.InstanceId,
	}, &output)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Error(err)
		}
	})
	injected, err := build(t.Context(), cfg, io.Discard, []deer.Option{deer.WithLogger(logger)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := injected.Close(t.Context()); err != nil {
			t.Error(err)
		}
	})
	if err := injected.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertLoggers(t, kratosBefore, hertzBefore)
	if bytes.Count(output.Bytes(), []byte(`"msg":"service.transports_stopped"`)) != stoppedBefore+1 {
		t.Fatal("Wire injector did not pass the borrowed logger to Deer")
	}
	if err := logger.LogAttrs(t.Context(), slog.LevelInfo, "borrowed logger remains usable"); err != nil {
		t.Fatal(err)
	}
}

func assertLoggers(t *testing.T, kratosBefore kratoslog.Logger, hertzBefore hlog.FullLogger) {
	t.Helper()
	if kratoslog.GetLogger() != kratosBefore || hlog.DefaultLogger() != hertzBefore {
		t.Fatal("application failed to restore process SDK logging")
	}
}
