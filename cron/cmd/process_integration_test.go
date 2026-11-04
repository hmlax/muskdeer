//go:build integration

package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	"github.com/hmlax/muskdeer/deer/testprocess"
)

func TestCronCommandLoadsNacosPairAndProvidesGRPCHealth(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	cfg, err := deer.LoadSplitConfig(ctx, filepath.Join("..", "..", "configs", "local", "cron"))
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
	cfg.Nacos = &configv1.Nacos{
		Address:        os.Getenv("MUSKDEER_TEST_NACOS_ADDRESS"),
		NamespaceId:    os.Getenv("MUSKDEER_TEST_NACOS_NAMESPACE"),
		Group:          "MUSKDEER_CRON_TEST",
		Cluster:        "TEST",
		Username:       os.Getenv("MUSKDEER_TEST_NACOS_USERNAME"),
		PasswordEnv:    "MUSKDEER_TEST_NACOS_PASSWORD",
		ConnectTimeout: durationpb.New(3 * time.Second),
		CommandTimeout: durationpb.New(time.Second),
		MaxInflight:    2,
		TlsEnabled:     proto.Bool(false),
	}
	body, err := protojson.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "fixture.json")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	run, cancelRun := context.WithTimeout(context.WithoutCancel(t.Context()), 30*time.Second)
	t.Cleanup(cancelRun)
	command, cleanup, err := testprocess.Command(run, "cron", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cleanup(); err != nil {
			t.Error(err)
		}
	})
	if len(command.Args) != 1 {
		t.Fatal("cron command received configuration flags")
	}
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	t.Cleanup(func() {
		if err := command.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Error(err)
		}
		if err := <-done; err != nil {
			t.Errorf("cron process failed: %v; %s", err, &output)
		}
	})
	connection, err := grpc.NewClient("passthrough:///"+cfg.Grpc.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := connection.Close(); err != nil {
			t.Error(err)
		}
	}()
	client := healthv1.NewHealthClient(connection)
	ticks := time.NewTicker(20 * time.Millisecond)
	defer ticks.Stop()
	for {
		call, cancelCall := context.WithTimeout(ctx, time.Second)
		reply, err := client.Check(call, &healthv1.HealthCheckRequest{Service: "cron.grpc"})
		cancelCall()
		if err == nil && reply.GetStatus() == healthv1.HealthCheckResponse_SERVING {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("cron never exposed serving gRPC health", ctx.Err())
		case <-ticks.C:
		}
	}
}
