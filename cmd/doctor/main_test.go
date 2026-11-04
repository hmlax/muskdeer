package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorExitStatusAndSafeOutput(t *testing.T) {
	t.Parallel()
	const secret = "do_not_echo_doctor_input"
	tests := []struct {
		name string
		body string
		code int
	}{
		{
			name: "valid core configuration",
			body: `{"name":"execution","instanceId":"execution-test","version":"test","startupTimeout":"2s","shutdownTimeout":"2s","grpc":{"address":"127.0.0.1:0","requestTimeout":"1s"}}`,
			code: 0,
		},
		{
			name: "retired HTTP configuration",
			body: `{"name":"api","instanceId":"api-test","version":"test","startupTimeout":"2s","shutdownTimeout":"2s","http":{"address":"127.0.0.1:0","requestTimeout":"1s"}}`,
			code: 1,
		},
		{
			name: "bad field hides value",
			body: `{"grpcClients":{"maxInflight":"` + secret + `"}}`,
			code: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(test.body), 0600); err != nil {
				t.Fatal(err)
			}
			var output, diagnostic bytes.Buffer
			code := run(t.Context(), []string{"-config", path}, &output, &diagnostic)
			if code != test.code || strings.Contains(output.String()+diagnostic.String(), secret) {
				t.Fatalf("doctor status=%d, stdout=%s, stderr=%s", code, &output, &diagnostic)
			}
			if code == 0 && !strings.Contains(output.String(), "syntax and limits validated") {
				t.Fatal("doctor did not state its actual validation scope")
			}
		})
	}
}

func TestDoctorAcceptsPairedYAMLDirectories(t *testing.T) {
	for _, name := range []string{"market", "strategy", "execution", "portfolio", "cron"} {
		t.Run(name, func(t *testing.T) {
			var output, diagnostic bytes.Buffer
			path := filepath.Join("..", "..", "configs", "local", name)
			if code := run(t.Context(), []string{"-config", path}, &output, &diagnostic); code != 0 {
				t.Fatalf("paired YAML doctor exited %d: %s", code, &diagnostic)
			}
		})
	}
}
