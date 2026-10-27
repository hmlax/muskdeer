package deer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

func TestLoadConfigResolvesPathsWithoutOpeningDependencies(t *testing.T) {
	t.Parallel()
	cfg := serviceConfig("file-config")
	cfg.Logging = &configv1.Logging{
		File: &configv1.LogFile{
			Filename:   "logs/service.log",
			MaxSizeMb:  1,
			MaxBackups: 2,
			MaxAgeDays: 1,
		},
	}
	cfg.GrpcSecurity = &configv1.GRPCServerSecurity{
		Mtls: &configv1.MutualTLS{
			CertificateFile: "certs/service.pem",
			PrivateKeyFile:  "certs/service-key.pem",
			RootCertFile:    "certs/ca.pem",
		},
		AllowedPeerServices: []string{"api.grpc"},
	}
	path := filepath.Join(t.TempDir(), "service.json")
	data, err := protojson.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Dir(path)
	if loaded.Logging.File.Filename != filepath.Join(base, "logs/service.log") || loaded.GrpcSecurity.Mtls.PrivateKeyFile != filepath.Join(base, "certs/service-key.pem") || loaded.GrpcSecurity.Mtls.RootCertFile != filepath.Join(base, "certs/ca.pem") {
		t.Fatalf("relative references = %v", loaded)
	}
	if _, err := os.Stat(filepath.Join(base, "logs")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("loading configuration created a log directory: %v", err)
	}
}

func TestLoadConfigRejectsInvalidInputWithoutEchoingValues(t *testing.T) {
	t.Parallel()
	const secret = "do_not_echo_this_config_value"
	badLevel := serviceConfig("bad-level")
	badLevel.Logging = &configv1.Logging{
		Level:  secret,
		Stdout: true,
	}
	badLevelJSON, err := protojson.Marshal(badLevel)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		body string
	}{
		{
			name: "validation error hides input value",
			body: string(badLevelJSON),
		},
		{
			name: "unknown password field",
			body: `{"password":"` + secret + `"}`,
		},
		{
			name: "wrong numeric field type",
			body: `{"grpcClients":{"maxInflight":"` + secret + `"}}`,
		},
		{
			name: "duplicate alias fields",
			body: `{"name":"market","instance_id":"a","instanceId":"b"}`,
		},
		{
			name: "trailing document",
			body: `{} {}`,
		},
		{
			name: "missing service identity",
			body: `{}`,
		},
		{
			name: "too large",
			body: strings.Repeat(" ", 1024*1024+1),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "invalid.json")
			if err := os.WriteFile(path, []byte(test.body), 0600); err != nil {
				t.Fatal(err)
			}
			if cfg, err := LoadConfig(t.Context(), path); err == nil || cfg != nil || strings.Contains(err.Error(), secret) {
				t.Fatalf("unsafe config rejection: config=%v, error=%v", cfg, err)
			}
		})
	}
}

func TestLoadConfigPreservesFilesystemAndCancellationCauses(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "missing.json")
	if _, err := LoadConfig(t.Context(), missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file cause = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := LoadConfig(ctx, missing); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled loading cause = %v", err)
	}
	if _, err := LoadConfig(t.Context(), t.TempDir()); err == nil {
		t.Fatal("config loader accepted a directory")
	}
}
