package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	kratoslog "github.com/go-kratos/kratos/v2/log"
	"github.com/shopspring/decimal"
	"go.uber.org/goleak"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

type wireRecord struct {
	Level       string `json:"level"`
	Message     string `json:"msg"`
	Service     string `json:"service"`
	Instance    string `json:"instance"`
	RequestID   string `json:"request_id"`
	Price       string `json:"price"`
	APIKey      string `json:"api_key"`
	Credentials struct {
		Value string `json:"value"`
	} `json:"credentials"`
}

type failingOutput struct{}

type wireError struct{}

func (wireError) Error() string                { return "safe failure" }
func (wireError) MarshalJSON() ([]byte, error) { return []byte(`"secret-error-payload"`), nil }

func (failingOutput) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestJSONAndKratosBridgeKeepIdentityAndDecimal(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	logger, err := New(t.Context(), nil, Identity{
		Service:  "strategy",
		Instance: "node-a",
	}, &output)
	if err != nil {
		t.Fatal(err)
	}
	price := decimal.RequireFromString("12345678901234567890.123456789012345678")
	if err := logger.Log(kratoslog.LevelInfo, "msg", "ready", "request_id", "req-1", "price", price, "service", "spoofed"); err != nil {
		t.Fatal(err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	var record wireRecord
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.Level != "INFO" || record.Message != "ready" || record.Service != "strategy" || record.Instance != "node-a" || record.RequestID != "req-1" || record.Price != price.String() {
		t.Fatalf("JSON log = %+v; want original identity and exact decimal string", record)
	}
}

func TestSensitiveFieldsAreRedactedAcrossGroups(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	logger, err := New(t.Context(), nil, Identity{
		Service:  "api",
		Instance: "node-a",
	}, &output)
	if err != nil {
		t.Fatal(err)
	}
	logger.Slog().With("api_key", "secret-api-key").WithGroup("credentials").InfoContext(t.Context(), "request rejected", "value", "secret-password")
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(output.Bytes(), []byte("secret-")) {
		t.Fatalf("sensitive log contents: %s", output.Bytes())
	}
	var record wireRecord
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.APIKey != "[REDACTED]" || record.Credentials.Value != "[REDACTED]" || record.Service != "api" {
		t.Fatalf("redacted JSON = %+v", record)
	}
}

func TestInlineGroupsCannotOverrideRootIdentity(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	logger, err := New(t.Context(), nil, Identity{
		Service:  "api",
		Instance: "original",
	}, &output)
	if err != nil {
		t.Fatal(err)
	}
	logger.Slog().With(slog.Group("", slog.String("service", "spoofed"))).InfoContext(t.Context(), "identity checked", slog.Group("", slog.Group("", slog.String("instance", "spoofed"))))
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	var record wireRecord
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.Service != "api" || record.Instance != "original" {
		t.Fatalf("inline group overrode identity: %+v", record)
	}
}

func TestReservedRootGroupsAndCaseVariantsKeepIdentity(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	logger, err := New(t.Context(), nil, Identity{
		Service:  "api",
		Instance: "original",
	}, &output)
	if err != nil {
		t.Fatal(err)
	}
	logger.Slog().With("Service", "spoofed").WithGroup("instance").InfoContext(t.Context(), "identity checked", "value", "nested")
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	var record wireRecord
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.Service != "api" || record.Instance != "original" {
		t.Fatalf("reserved group or case variant overrode identity: %+v", record)
	}
}

func TestErrorsDoNotInvokeArbitraryJSONSerialization(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	logger, err := New(t.Context(), nil, Identity{
		Service:  "execution",
		Instance: "typed-error",
	}, &output)
	if err != nil {
		t.Fatal(err)
	}
	if err := logger.LogAttrs(t.Context(), slog.LevelError, "operation failed", slog.Any("err", wireError{})); err != nil {
		t.Fatal(err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(output.Bytes(), []byte("secret-error-payload")) || !bytes.Contains(output.Bytes(), []byte("safe failure")) {
		t.Fatalf("error JSON serialization escaped the log policy: %s", output.Bytes())
	}
}

func TestLoggerClosePreservesOutputFailure(t *testing.T) {
	t.Parallel()
	logger, err := New(t.Context(), nil, Identity{
		Service:  "api",
		Instance: "write-failure",
	}, failingOutput{})
	if err != nil {
		t.Fatal(err)
	}
	if err := logger.Log(kratoslog.LevelError, "msg", "dependency unavailable"); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("Log failure = %v", err)
	}
	for range 2 {
		if err := logger.Close(); !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("Close failure = %v", err)
		}
	}
}

func TestInstancesOwnSeparateFilesAndDoNotChangeGlobalLogger(t *testing.T) {
	t.Parallel()
	previous := slog.Default()
	config := fileConfig(t)
	first, err := New(t.Context(), config, Identity{
		Service:  "strategy",
		Instance: "node-a",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(t.Context(), config, Identity{
		Service:  "strategy",
		Instance: "node-b",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Filename() == second.Filename() {
		t.Fatal("distinct instances must not write the same file")
	}
	for _, logger := range []*Logger{first, second} {
		if err := logger.LogAttrs(t.Context(), slog.LevelInfo, "ready"); err != nil {
			t.Fatal(err)
		}
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := second.LogAttrs(t.Context(), slog.LevelInfo, "still running"); err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if slog.Default() != previous {
		t.Fatal("constructing instances changed the process-global logger")
	}
	data, err := os.ReadFile(second.Filename())
	if err != nil || bytes.Count(data, []byte("\n")) != 2 {
		t.Fatalf("second instance log = %q, %v", data, err)
	}
}

func TestFileOwnershipRejectsDuplicateWriterAndReleasesLock(t *testing.T) {
	t.Parallel()
	config := fileConfig(t)
	identity := Identity{
		Service:  "api",
		Instance: "same-instance",
	}
	first, err := New(t.Context(), config, identity, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(t.Context(), config, identity, nil)
	if err == nil || second != nil {
		t.Fatal("simultaneous writers with the same file identity must be rejected")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err = New(t.Context(), config, identity, nil)
	if err != nil {
		t.Fatalf("file lock not released by Close: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentRecordsAndClosedLogger(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	logger, err := New(t.Context(), nil, Identity{
		Service:  "market",
		Instance: "concurrent",
	}, &output)
	if err != nil {
		t.Fatal(err)
	}
	var writers sync.WaitGroup
	for range 8 {
		writers.Go(func() {
			for range 20 {
				if err := logger.LogAttrs(t.Context(), slog.LevelInfo, "record written"); err != nil {
					t.Error(err)
				}
			}
		})
	}
	writers.Wait()
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	for range 160 {
		var record wireRecord
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
	}
	if err := logger.LogAttrs(t.Context(), slog.LevelInfo, "after close"); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("Log after Close = %v", err)
	}
}

func TestInvalidConfigFailsBeforeCreatingFiles(t *testing.T) {
	t.Parallel()
	config := fileConfig(t)
	config.File.MaxSizeMb = 0
	logger, err := New(context.WithoutCancel(t.Context()), config, Identity{
		Service:  "api",
		Instance: "invalid",
	}, nil)
	if err == nil || logger != nil {
		t.Fatal("zero max_size_mb must be rejected")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(config.File.Filename), "api")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid config created files: %v", err)
	}
}

func TestLevelFilteringAndStdoutFileAgreement(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	config := fileConfig(t)
	config.Stdout = true
	config.Level = "warn"
	logger, err := New(t.Context(), config, Identity{
		Service:  "portfolio",
		Instance: "levels",
	}, &output)
	if err != nil {
		t.Fatal(err)
	}
	logger.Slog().InfoContext(t.Context(), "filtered record")
	logger.Slog().WarnContext(t.Context(), "kept record")
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(logger.Filename())
	if err != nil || !bytes.Equal(output.Bytes(), data) || strings.Contains(string(data), "filtered record") || !strings.Contains(string(data), "kept record") {
		t.Fatalf("multi-output log = %q, %v", data, err)
	}
}

func fileConfig(t *testing.T) *configv1.Logging {
	t.Helper()
	return &configv1.Logging{
		Level: "info",
		File: &configv1.LogFile{
			Filename:   filepath.Join(t.TempDir(), "service.log"),
			MaxSizeMb:  1,
			MaxBackups: 2,
			MaxAgeDays: 1,
			Compress:   true,
		},
	}
}
