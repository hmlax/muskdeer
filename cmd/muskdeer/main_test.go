package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

type processRecord struct {
	Message           string `json:"msg"`
	Service           string `json:"service"`
	Instance          string `json:"instance"`
	MonitoringAddress string `json:"monitoring_address"`
	Framework         string `json:"framework"`
}

func TestHelperServiceProcess(_ *testing.T) {
	if os.Getenv("MUSKDEER_TEST_SERVICE_CHILD") != "1" {
		return
	}
	os.Args = []string{"muskdeer", "-role", os.Getenv("MUSKDEER_TEST_SERVICE_ROLE"), "-config", os.Getenv("MUSKDEER_TEST_SERVICE_CONFIG")}
	main()
}

func TestEachServiceRoleOwnsAnIndependentProcess(t *testing.T) {
	for _, role := range []string{"api", "market", "strategy", "execution", "portfolio", "cron"} {
		t.Run(role, func(t *testing.T) {
			t.Parallel()
			cfg := &configv1.ServiceConfig{
				Name:            role,
				InstanceId:      role + "-child",
				Version:         "test",
				StartupTimeout:  durationpb.New(3 * time.Second),
				ShutdownTimeout: durationpb.New(3 * time.Second),
				Monitoring: &configv1.Listener{
					Address:        "127.0.0.1:0",
					RequestTimeout: durationpb.New(time.Second),
				},
			}
			if role == "api" {
				cfg.Http = cfg.Monitoring
			} else if role != "cron" {
				cfg.Grpc = cfg.Monitoring
			}
			data, err := protojson.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "service.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			command := exec.CommandContext(ctx, executable, "-test.run=^TestHelperServiceProcess$")
			command.Env = append(os.Environ(), "MUSKDEER_TEST_SERVICE_CHILD=1", "MUSKDEER_TEST_SERVICE_ROLE="+role, "MUSKDEER_TEST_SERVICE_CONFIG="+path)
			var diagnostic bytes.Buffer
			command.Stderr = &diagnostic
			stdout, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			initialized := make(chan processRecord, 1)
			finished := make(chan error, 1)
			scanExited := make(chan struct{})
			var framework bool
			go func() {
				defer close(scanExited)
				scanner := bufio.NewScanner(stdout)
				for scanner.Scan() {
					var record processRecord
					if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
						finished <- fmt.Errorf("non-JSON process output: %w", err)
						return
					}
					if record.Service != role || record.Instance != cfg.InstanceId {
						finished <- fmt.Errorf("process identity changed: %+v", record)
						return
					}
					if record.Framework != "" {
						framework = true
					}
					if record.Message == "service.initialized" {
						initialized <- record
					}
				}
				finished <- scanner.Err()
			}()
			var waitOnce sync.Once
			finish := func() {
				waitOnce.Do(func() {
					cancel()
					<-scanExited
					if err := command.Wait(); err != nil && command.ProcessState == nil {
						t.Error(err)
					}
				})
			}
			t.Cleanup(finish)
			var record processRecord
			select {
			case record = <-initialized:
			case err := <-finished:
				t.Fatalf("process did not initialize: %v", err)
			case <-ctx.Done():
				t.Fatal("process initialization exceeded its test budget")
			}
			waitProcessReady(ctx, t, record.MonitoringAddress)
			if err := command.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			if err := <-finished; err != nil {
				t.Fatal(err)
			}
			<-scanExited
			waitOnce.Do(func() {
				if err := command.Wait(); err != nil {
					t.Errorf("service termination: %v; diagnostic=%s", err, &diagnostic)
				}
			})
			if !framework || diagnostic.Len() != 0 {
				t.Fatalf("framework bridge missing or wrote outside JSON logger: framework=%v stderr=%s", framework, &diagnostic)
			}
		})
	}
}

func TestCancellationDoesNotHideShutdownFailure(t *testing.T) {
	if !onlyCancellation(errors.Join(context.Canceled, fmt.Errorf("wrapped: %w", context.Canceled))) {
		t.Fatal("pure cancellation was not recognized")
	}
	if onlyCancellation(errors.Join(context.Canceled, errors.New("cleanup failed"))) || onlyCancellation(context.DeadlineExceeded) {
		t.Fatal("termination suppressed an independent failure")
	}
}

func waitProcessReady(ctx context.Context, t *testing.T, address string) {
	t.Helper()
	client := &http.Client{Timeout: 100 * time.Millisecond}
	defer client.CloseIdleConnections()
	ticks := time.NewTicker(20 * time.Millisecond)
	defer ticks.Stop()
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/readyz", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err == nil {
			closeErr := response.Body.Close()
			if closeErr != nil {
				t.Fatal(closeErr)
			}
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("service did not become ready")
		case <-ticks.C:
		}
	}
}
