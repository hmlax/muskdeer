//go:build integration

package infrastructureprobe

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestIndependentProcessesPublishServeAndWithdraw(t *testing.T) {
	ca := testcert.New(t)
	clientIdentity := ca.Issue(t, "api.grpc", false)
	pidA, stopA := startServiceProcess(t, ca, "process-a")
	pidB, stopB := startServiceProcess(t, ca, "process-b")
	if pidA == pidB || pidA == os.Getpid() || pidB == os.Getpid() {
		t.Fatal("services did not receive independent process identities")
	}
	observer := namingObserver(t)
	instances := waitNamingCount(t, observer, "market.grpc", 2)
	for _, instance := range instances {
		callProcessHealth(t, clientIdentity, instance)
	}
	stopA()
	remaining := waitNamingCount(t, observer, "market.grpc", 1)
	if remaining[0].ID != "process-b" {
		t.Fatalf("stopping A changed B's discovery identity: %+v", remaining)
	}
	callProcessHealth(t, clientIdentity, remaining[0])
	stopB()
	waitNamingCount(t, observer, "market.grpc", 0)
}

func startServiceProcess(t *testing.T, ca *testcert.Authority, id string) (int, func()) {
	t.Helper()
	cfg := serviceConfig(t, id)
	cfg.Name = "market"
	cfg.Postgres = nil
	cfg.Monitoring = &configv1.Listener{
		Address:        "127.0.0.1:0",
		RequestTimeout: durationpb.New(time.Second),
	}
	cfg.Nacos = namingConfig()
	cfg.Nacos.Registration = &configv1.NacosRegistration{
		AdvertiseIp:       "127.0.0.1",
		HeartbeatInterval: durationpb.New(time.Second),
	}
	cfg.GrpcSecurity = &configv1.GRPCServerSecurity{
		Mtls:                ca.Issue(t, "market.grpc", false),
		AllowedPeerServices: []string{"api.grpc"},
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "service.json")
	data, err := protojson.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := os.Create(filepath.Join(directory, "stdout.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	diagnostic, err := os.Create(filepath.Join(directory, "stderr.jsonl"))
	if err != nil {
		if closeErr := output.Close(); closeErr != nil {
			t.Error(closeErr)
		}
		t.Fatal(err)
	}
	// Cleanup owns SIGTERM and joining, including when the parent test fails.
	// Keep the original 15s hard process bound without a premature Go-test
	// cancellation killing the child before its owned cleanup can run.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 15*time.Second)
	command := exec.CommandContext(ctx, filepath.Join("..", "..", ".cache", "bin", "muskdeer"), "-role", "market", "-config", path)
	command.Stdout = output
	command.Stderr = diagnostic
	completed := make(chan struct{})
	var result error
	var stopped bool
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		defer cancel()
		if command.Process != nil {
			if err := command.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
				t.Error(err)
			}
			select {
			case <-completed:
			case <-time.After(5 * time.Second):
				cancel()
				<-completed
				t.Error("service exceeded its shutdown budget")
			}
			if result != nil {
				t.Errorf("service %s exit: %v", id, result)
			}
		}
		for _, file := range []*os.File{output, diagnostic} {
			if err := file.Close(); err != nil {
				t.Error(err)
			}
		}
		assertProcessLogs(t, output.Name(), diagnostic.Name(), id)
	}
	t.Cleanup(stop)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		result = command.Wait()
		close(completed)
	}()
	return command.Process.Pid, stop
}

func assertProcessLogs(t *testing.T, outputPath, diagnosticPath, id string) {
	t.Helper()
	diagnostic, err := os.ReadFile(diagnosticPath)
	if err != nil || len(diagnostic) != 0 {
		t.Errorf("service %s stderr: %s, %v", id, diagnostic, err)
	}
	output, err := os.Open(outputPath)
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		if err := output.Close(); err != nil {
			t.Error(err)
		}
	}()
	var initialized, stopped, framework bool
	scanner := bufio.NewScanner(output)
	for scanner.Scan() {
		var record struct {
			Service   string `json:"service"`
			Instance  string `json:"instance"`
			Message   string `json:"msg"`
			Framework string `json:"framework"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Errorf("non-JSON service output: %v", err)
			continue
		}
		if record.Service != "market" || record.Instance != id {
			t.Errorf("process logger lost identity: %+v", record)
		}
		initialized = initialized || record.Message == "service.initialized"
		stopped = stopped || record.Message == "service.transports_stopped"
		framework = framework || record.Framework != ""
	}
	if err := scanner.Err(); err != nil {
		t.Error(err)
	}
	if !initialized || !stopped || !framework {
		t.Errorf("missing process lifecycle logs: initialized=%v stopped=%v framework=%v", initialized, stopped, framework)
	}
}

func callProcessHealth(t *testing.T, identity *configv1.MutualTLS, instance nacos.Instance) {
	t.Helper()
	tlsConfig, err := rpcsecurity.Client(identity, "api.grpc", "market.grpc")
	if err != nil {
		t.Fatal(err)
	}
	connection, err := grpc.NewClient("passthrough:///"+instance.Address,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)), grpc.WithDisableRetry())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := connection.Close(); err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	reply, err := healthv1.NewHealthClient(connection).Check(ctx, &healthv1.HealthCheckRequest{Service: "market.grpc"})
	if err != nil || reply.GetStatus() != healthv1.HealthCheckResponse_SERVING {
		t.Fatalf("discovered process %s health: %v, %v", instance.ID, reply, err)
	}
}
