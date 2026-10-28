//go:build integration

package migrate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"
	"gorm.io/gorm"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type nativeSessionReply struct {
	Session struct {
		UserID      string           `json:"user_id"`
		Username    string           `json:"username"`
		Role        domain.AdminRole `json:"role"`
		SessionID   string           `json:"session_id"`
		ExpiresAtMS string           `json:"expires_at_ms"`
	} `json:"session"`
}

func nativeAdminFixture(ctx context.Context, t *testing.T) (*gorm.DB, string) {
	t.Helper()
	db, postgres := migrationDatabase(t)
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	cfg := &configv1.ServiceConfig{
		Name:            "api",
		InstanceId:      "admin-process-test",
		Version:         "test",
		StartupTimeout:  durationpb.New(5 * time.Second),
		ShutdownTimeout: durationpb.New(5 * time.Second),
		Http: &configv1.Listener{
			Address:        "127.0.0.1:0",
			RequestTimeout: durationpb.New(3 * time.Second),
		},
		Monitoring: &configv1.Listener{
			Address:        "127.0.0.1:0",
			RequestTimeout: durationpb.New(time.Second),
		},
		Postgres: postgres,
		AdminHttp: &configv1.AdminHTTP{
			Enabled:               true,
			PublicOrigin:          "http://127.0.0.1:0",
			AllowInsecureLoopback: true,
		},
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "api.json")
	data, err := protojson.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	bootstrap := exec.CommandContext(ctx, filepath.Join("..", "..", ".cache", "bin", "admin"), "-config", path, "-username", "native-viewer", "-role", string(domain.AdminRoleViewer), "-password-env", "MUSKDEER_NATIVE_ADMIN_PASSWORD")
	bootstrap.Env = append(os.Environ(), "MUSKDEER_NATIVE_ADMIN_PASSWORD="+adminFixturePassword)
	output, err := bootstrap.CombinedOutput()
	if err != nil || bytes.Contains(output, []byte(adminFixturePassword)) {
		t.Fatal("actual identity command failed or leaked its password")
	}
	base := startNativeAdminAPI(ctx, t, path, directory)
	return db, base
}

func TestAdminCommandAndAPIProcessLoginSessionAndLogout(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	db, base := nativeAdminFixture(ctx, t)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{
		MaxIdleConns:    2,
		MaxConnsPerHost: 2,
	}
	// Registered after the owned API: client sockets close before its SIGTERM.
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{
		Jar:       jar,
		Timeout:   3 * time.Second,
		Transport: transport,
	}
	body := `{"username":"native-viewer","password":"` + adminFixturePassword + `"}`
	reply := nativeAdminRequest(ctx, t, client, base, "POST", "/api/v1/auth/login", body, base, "")
	if reply.StatusCode != http.StatusOK {
		t.Fatal("actual API login failed")
	}
	var session nativeSessionReply
	if err := json.NewDecoder(io.LimitReader(reply.Body, 4096)).Decode(&session); err != nil {
		t.Fatal(err)
	}
	if err := reply.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if session.Session.Role != domain.AdminRoleViewer || session.Session.UserID == "" || session.Session.ExpiresAtMS == "" {
		t.Fatal("actual API session was not typed or role-bound")
	}
	var csrf string
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range jar.Cookies(parsed) {
		if cookie.Name == "muskdeer_csrf" {
			csrf = cookie.Value
		}
	}
	if csrf == "" {
		t.Fatal("actual API omitted independent CSRF")
	}
	for _, check := range []struct {
		method string
		path   string
		origin string
		csrf   string
		status int
	}{
		{
			method: "GET",
			path:   "/api/v1/auth/session",
			status: 200,
		},
		{
			method: "POST",
			path:   "/api/v1/auth/logout",
			origin: "https://outside.example.test",
			csrf:   csrf,
			status: 403,
		},
		{
			method: "POST",
			path:   "/api/v1/auth/logout",
			origin: base,
			status: 403,
		},
		{
			method: "POST",
			path:   "/api/v1/auth/logout",
			origin: base,
			csrf:   csrf,
			status: 204,
		},
		{
			method: "GET",
			path:   "/api/v1/auth/session",
			status: 401,
		},
		{
			method: "GET",
			path:   "/api/v1/not-present",
			status: 404,
		},
	} {
		response := nativeAdminRequest(ctx, t, client, base, check.method, check.path, "", check.origin, check.csrf)
		if response.StatusCode != check.status {
			t.Errorf("actual API method=%s path=%s status=%d expected=%d", check.method, check.path, response.StatusCode, check.status)
		}
		if err := response.Body.Close(); err != nil {
			t.Fatal(err)
		}
	}
	var audits int64
	if err := db.WithContext(ctx).Model(&modelv1.AdminAudit{}).Count(&audits).Error; err != nil || audits != 3 {
		t.Fatal("actual command/login/logout were not independently audited")
	}
}

type nativeAdminHeader struct {
	Key   string
	Value string
}

func nativeAdminRequest(ctx context.Context, t *testing.T, client *http.Client, base, method, path, body, origin, csrf string, headers ...nativeAdminHeader) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, method, base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	for _, header := range headers {
		request.Header.Set(header.Key, header.Value)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func startNativeAdminAPI(ctx context.Context, t *testing.T, path, directory string) string {
	t.Helper()
	return startNativeAdminAPIFor(ctx, t, path, directory, 40*time.Second)
}

func startNativeAdminAPIFor(ctx context.Context, t *testing.T, path, directory string, lifetime time.Duration) string {
	t.Helper()
	if lifetime <= 0 || lifetime > 3*time.Minute {
		t.Fatal("native management API requires a bounded test lifetime")
	}
	logs := filepath.Join(directory, "api.jsonl")
	output, err := os.OpenFile(logs, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	run, cancelRun := context.WithTimeout(context.WithoutCancel(t.Context()), lifetime)
	command := exec.CommandContext(run, filepath.Join("..", "..", ".cache", "bin", "muskdeer"), "-role", "api", "-config", path)
	command.Stdout, command.Stderr = output, output
	finished := make(chan error, 1)
	if err := command.Start(); err != nil {
		cancelRun()
		if closeErr := output.Close(); closeErr != nil {
			t.Error(closeErr)
		}
		t.Fatal(err)
	}
	go func() { finished <- command.Wait() }()
	t.Cleanup(func() {
		defer cancelRun()
		if err := command.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Error(err)
		}
		timer := time.NewTimer(6 * time.Second)
		defer timer.Stop()
		select {
		case err := <-finished:
			if err != nil {
				t.Error("actual API process exited unsuccessfully")
			}
		case <-timer.C:
			t.Error("actual API process exceeded shutdown budget")
			if err := command.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				t.Error(err)
			}
			if err := <-finished; err != nil {
				t.Log("actual API process joined after forced stop")
			}
		}
		if err := output.Close(); err != nil {
			t.Error(err)
		}
		if t.Failed() {
			if data, err := os.ReadFile(logs); err == nil {
				t.Log(string(data))
			}
		}
	})
	ready, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticks := time.NewTicker(20 * time.Millisecond)
	defer ticks.Stop()
	for {
		if data, err := os.ReadFile(logs); err == nil {
			for _, line := range bytes.Split(data, []byte("\n")) {
				var event struct {
					Message     string `json:"msg"`
					HTTPAddress string `json:"http_address"`
				}
				if json.Unmarshal(line, &event) == nil && event.Message == "service.initialized" && event.HTTPAddress != "" {
					return "http://" + event.HTTPAddress
				}
			}
		}
		select {
		case <-ready.Done():
			t.Fatal("actual API did not initialize within startup budget")
		case <-ticks.C:
		}
	}
}
