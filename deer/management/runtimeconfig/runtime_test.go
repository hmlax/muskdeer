package runtimeconfig

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

const testServerConfig = `server:
  rest:
    addr: ':7788'
  grpc:
    addr: ':7790'
    timeout: 17s
runtime:
  sso:
    issuer: https://idp.example.com
`
const testDataConfig = `data:
  database:
    driver: postgres
    source: 'postgres://project-db'
  redis:
    addr: nacos-redis:6379
    db: 12
authn:
  jwt:
    method: HS256
    key: nacos-test-signing-key
runtime:
  crypto_key: 0123456789abcdef
`

type configFixture struct {
	documents map[string]string
	err       error
	requested []string
}

func (f *configFixture) GetConfig(in vo.ConfigParam) (string, error) {
	f.requested = append(f.requested, in.DataId)
	return f.documents[in.DataId], f.err
}
func TestNacosIsTheOnlyApplicationConfigurationSource(t *testing.T) {
	for key, value := range map[string]string{
		"HTTP_ADDR": "wrong:1", "REDIS_ADDR": "wrong:2", "REDIS_DB": "invalid",
		"GWA_AUTH_JWT_KEY": "wrong-key", "DB_DSN": "wrong-db", "GO_ADMIN_CRYPTO_KEY": "wrong-crypto",
		"CONFIG_DIR": "/missing/config", "CONFIG_YAML": "not: [valid", "NACOS_DATA_ID": "ignored.yaml",
	} {
		t.Setenv(key, value)
	}
	fixture := &configFixture{documents: map[string]string{"server.yaml": testServerConfig, "data.yaml": testDataConfig}}
	cfg, err := loadDocuments(fixture, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(fixture.requested, ",") != "server.yaml,data.yaml" {
		t.Fatalf("requested %v", fixture.requested)
	}
	if cfg.GetServer().GetRest().GetAddr() != ":7788" || cfg.GetServer().GetGrpc().GetTimeout().AsDuration() != 17*time.Second {
		t.Fatal("transport did not use Nacos")
	}
	if cfg.GetData().GetRedis().GetAddr() != "nacos-redis:6379" || cfg.GetData().GetRedis().GetDb() != 12 || cfg.GetData().GetDatabase().GetSource() != "postgres://project-db" {
		t.Fatal("data did not use Nacos")
	}
	if cfg.GetAuthn().GetJwt().GetKey() != "nacos-test-signing-key" || cfg.Runtime.CryptoKey != "0123456789abcdef" || cfg.Runtime.SSO.Issuer != "https://idp.example.com" {
		t.Fatal("security settings did not use Nacos")
	}
	_, err = loadDocuments(fixture, "admin")
	if err != nil {
		t.Fatal(err)
	}
}
func TestMissingOrUnavailableNacosConfigurationFailsStartup(t *testing.T) {
	for _, missing := range []string{"server.yaml", "data.yaml"} {
		t.Run(missing, func(t *testing.T) {
			fixture := &configFixture{documents: map[string]string{"server.yaml": testServerConfig, "data.yaml": testDataConfig}}
			delete(fixture.documents, missing)
			if _, err := loadDocuments(fixture, "operator"); err == nil || !strings.Contains(err.Error(), missing) {
				t.Fatalf("missing document: %v", err)
			}
		})
	}
	unavailable := errors.New("configuration server unavailable")
	if _, err := loadDocuments(&configFixture{err: unavailable}, "operator"); !errors.Is(err, unavailable) {
		t.Fatalf("original error lost: %v", err)
	}
	t.Setenv("NACOS_ADDR", "")
	if _, _, _, err := Load("admin"); err == nil {
		t.Fatal("missing Nacos connection must fail startup")
	}
}
func TestInvalidApplicationSettingsAreRejected(t *testing.T) {
	for _, document := range []string{"runtime:\n  smtp:\n    port: invalid\n", "runtime:\n  unknown_option: true\n"} {
		if _, err := Decode([]byte(document)); err == nil {
			t.Fatal("invalid runtime settings accepted")
		}
	}
	fixture := &configFixture{documents: map[string]string{"server.yaml": testServerConfig, "data.yaml": strings.ReplaceAll(testDataConfig, "nacos-test-signing-key", "")}}
	if _, err := loadDocuments(fixture, "operator"); err == nil {
		t.Fatal("empty JWT signing configuration accepted")
	}
}
func TestNacosBootstrapEnvironment(t *testing.T) {
	t.Setenv("NACOS_ADDR", "http://127.0.0.1:8848,https://nacos.example:8849")
	t.Setenv("NACOS_NAMESPACE_ID", "test-namespace")
	t.Setenv("NACOS_USERNAME", "test-user")
	t.Setenv("NACOS_PASSWORD", "test-password")
	param, err := nacosParam()
	if err != nil {
		t.Fatal(err)
	}
	if len(param.ServerConfigs) != 2 || param.ServerConfigs[1].Scheme != "https" || param.ServerConfigs[1].Port != 8849 {
		t.Fatal("Nacos addresses not parsed")
	}
	if param.ClientConfig.NamespaceId != "test-namespace" || param.ClientConfig.Username != "test-user" || param.ClientConfig.Password != "test-password" || !param.ClientConfig.NotLoadCacheAtStart || !param.ClientConfig.DisableUseSnapShot {
		t.Fatal("Nacos bootstrap settings lost")
	}
}

func TestNacosOwnsTracingSettings(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://wrong:1")
	t.Setenv("OTEL_TRACES_SAMPLER", "always_off")
	server := strings.Replace(testServerConfig, "  sso:", "  tracing:\n    enabled: true\n    endpoint: tracing.example.test:4317\n    insecure: false\n    sampling_per_million: 100000\n    export_timeout: 3s\n  sso:", 1)
	cfg, err := Decode([]byte(server), []byte(testDataConfig))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Runtime.Tracing.Endpoint != "tracing.example.test:4317" || cfg.Runtime.Tracing.ExportTimeout != 3*time.Second {
		t.Fatal("Nacos tracing settings were lost or overridden")
	}
	cfg.Runtime.Tracing.SamplingPerMillion = 1000001
	if validate(cfg, "admin") == nil {
		t.Fatal("invalid tracing sampler accepted")
	}
}
