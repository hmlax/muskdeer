package deer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
)

const nacosConfigFixture = `{"name":"strategy","instance_id":"strategy-config-test","version":"test","startup_timeout":"5s","shutdown_timeout":"5s","grpc":{"address":"127.0.0.1:0","request_timeout":"1s"},"monitoring":{"address":"127.0.0.1:59103","request_timeout":"1s"},"venue_connection":{"target":"127.0.0.1:8080","token_env":"NACOS_UNIT_GATEWAY_TOKEN"}}`

func TestNacosConfigurationUsesEnvironmentScopeAndTypedContent(t *testing.T) {
	var original configv1.ServiceConfig
	if err := protojson.Unmarshal([]byte(nacosConfigFixture), &original); err != nil {
		t.Fatal(err)
	}
	data, service, err := EncodeConfigPair(&original)
	if err != nil {
		t.Fatal(err)
	}
	server := configServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/nacos/v2/cs/config" || (r.URL.Query().Get("dataId") != "strategy-test.data.yaml" && r.URL.Query().Get("dataId") != "strategy-test.service.yaml") ||
			r.URL.Query().Get("namespaceId") != "unit-config" || r.URL.Query().Get("group") != "CORE_TEST" ||
			r.Header.Get("Authorization") != "Bearer unit-config-token" || r.URL.Query().Has("accessToken") {
			t.Error("configuration read lost its explicit authenticated scope")
		}
		if r.URL.Query().Get("dataId") == "strategy-test.data.yaml" {
			writeConfig(t, w, string(data))
		} else {
			writeConfig(t, w, string(service))
		}
	})
	setNacosEnvironment(t, server.URL)
	cfg, err := LoadNacosConfig(t.Context(), "strategy")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "strategy" || cfg.Business != nil || cfg.GetVenueConnection().GetTarget() != "127.0.0.1:8080" ||
		cfg.Nacos.Address != strings.TrimPrefix(server.URL, "http://") || cfg.Nacos.PasswordEnv != "NACOS_PASSWORD" {
		t.Fatal("configuration did not preserve infrastructure content and environment connection")
	}
}

func TestNacosConfigurationFailsClosedOnMissingScopeOrInvalidContent(t *testing.T) {
	for _, content := range []string{
		"",
		`{"name":"strategy","unknown_business_secret":"never-log-this"}`,
		strings.Replace(nacosConfigFixture, `"name":"strategy"`, `"name":"execution"`, 1),
		strings.Replace(nacosConfigFixture, `"venue_connection":`, `"nacos":{"address":"remote-override:8848"},"venue_connection":`, 1),
	} {
		t.Run("reject", func(t *testing.T) {
			server := configServer(t, func(w http.ResponseWriter, _ *http.Request) { writeConfig(t, w, content) })
			setNacosEnvironment(t, server.URL)
			_, err := LoadNacosConfig(t.Context(), "strategy")
			if err == nil || strings.Contains(err.Error(), "never-log-this") || strings.Contains(err.Error(), "remote-override") {
				t.Fatal("invalid or foreign configuration was accepted or exposed")
			}
		})
	}
	t.Setenv("NACOS_ADDRESS", "")
	if _, err := LoadNacosConfig(t.Context(), "strategy"); err == nil {
		t.Fatal("missing environment bootstrap was accepted")
	}
}

func TestNacosConfigurationHonorsCancellation(t *testing.T) {
	server := configServer(t, func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	setNacosEnvironment(t, server.URL)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, err := LoadNacosConfig(ctx, "strategy"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("configuration read lost caller deadline: %v", err)
	}
}

func configServer(t *testing.T, config http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/nacos/v1/auth/login" {
			if err := r.ParseForm(); err != nil || r.Form.Get("password") != "unit-config-password" {
				t.Error("configuration login ignored the environment credential")
			}
			if _, err := w.Write([]byte(`{"accessToken":"unit-config-token","tokenTtl":18000}`)); err != nil {
				t.Error(err)
			}
			return
		}
		config(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func setNacosEnvironment(t *testing.T, address string) {
	t.Helper()
	for key, value := range map[string]string{
		"NACOS_ADDRESS":   strings.TrimPrefix(address, "http://"),
		"NACOS_NAMESPACE": "unit-config", "NACOS_GROUP": "CORE_TEST", "NACOS_CLUSTER": "TEST",
		"NACOS_USERNAME": "config-reader", "NACOS_PASSWORD": "unit-config-password",
		"NACOS_DATA_ID": "", "NACOS_TLS_ENABLED": "false",
		"NACOS_DATA_CONFIG_ID": "strategy-test.data.yaml", "NACOS_SERVICE_CONFIG_ID": "strategy-test.service.yaml",
		"NACOS_ROOT_CERT_FILE": "", "NACOS_CONNECT_TIMEOUT": "", "NACOS_COMMAND_TIMEOUT": "",
	} {
		t.Setenv(key, value)
	}
}

func writeConfig(t *testing.T, w http.ResponseWriter, content string) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(struct {
		Code int    `json:"code"`
		Data string `json:"data"`
	}{
		Code: 0,
		Data: content,
	}); err != nil {
		t.Error(err)
	}
}
