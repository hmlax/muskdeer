package testapp

import (
	"context"
	"errors"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"github.com/hmlax/muskdeer/deer"
)

// AssertGRPCHealth runs the generated service graph and probes its named health API.
func AssertGRPCHealth(t *testing.T, name string, build Build) {
	t.Helper()
	cfg, err := deer.LoadSplitConfig(t.Context(), filepath.Join("..", "..", "configs", "local", name))
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Grpc.Address = reserved.Addr().String()
	if err := reserved.Close(); err != nil {
		t.Fatal(err)
	}
	cfg.Monitoring.Address = "127.0.0.1:0"
	application, err := build(t.Context(), cfg, io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	run, cancelRun := context.WithCancel(t.Context())
	go func() { done <- application.Run(run) }()
	t.Cleanup(func() {
		cancelRun()
		if err := application.Close(t.Context()); err != nil {
			t.Error(err)
		}
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Error(err)
		}
	})
	conn, err := grpc.NewClient("passthrough:///"+cfg.Grpc.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	client := healthv1.NewHealthClient(conn)
	ticks := time.NewTicker(10 * time.Millisecond)
	defer ticks.Stop()
	for {
		result, err := client.Check(ctx, &healthv1.HealthCheckRequest{Service: name + ".grpc"})
		if err == nil && result.GetStatus() == healthv1.HealthCheckResponse_SERVING {
			break
		}
		if err != nil && status.Code(err) != codes.Unavailable {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("service did not expose serving gRPC health", ctx.Err())
		case <-ticks.C:
		}
	}
	if _, err := client.Check(ctx, &healthv1.HealthCheckRequest{Service: "unknown.grpc"}); status.Code(err) != codes.NotFound {
		t.Fatal("unknown service health was accepted", err)
	}
}
