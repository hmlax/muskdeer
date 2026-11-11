package deer

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"testing"
)

func TestNacosLoadsSeparateDataAndServiceYAML(t *testing.T) {
	data := []byte("name: strategy\nversion: test\nvenue_connection:\n  target: 127.0.0.1:8080\n  token_env: TEST_GATEWAY_TOKEN\n")
	service := fmt.Sprintf("name: strategy\ninstance_id: split-config-test\nversion: test\nstartup_timeout: 5s\nshutdown_timeout: 5s\ngrpc:\n  address: '127.0.0.1:0'\n  request_timeout: 1s\ntracing:\n  enabled: true\n  endpoint: tracing.example.test:4317\n  insecure: false\n  sampling_per_million: 100000\n  export_timeout: 3s\ndata_sha256: '%x'\n", sha256.Sum256(data))
	server := configServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("dataId") {
		case "strategy.data.yaml":
			writeConfig(t, w, string(data))
		case "strategy.service.yaml":
			writeConfig(t, w, service)
		default:
			http.NotFound(w, r)
		}
	})
	setNacosEnvironment(t, server.URL)
	t.Setenv("OTEL_SERVICE_NAME", "ignored")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://wrong:1")
	t.Setenv("NACOS_DATA_ID", "")
	t.Setenv("NACOS_DATA_CONFIG_ID", "strategy.data.yaml")
	t.Setenv("NACOS_SERVICE_CONFIG_ID", "strategy.service.yaml")
	cfg, err := LoadNacosConfig(t.Context(), "strategy")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GetTracing().GetEndpoint() != "tracing.example.test:4317" || cfg.GetTracing().GetSamplingPerMillion() != 100000 {
		t.Fatal("tracing was not loaded from the paired Nacos service document")
	}
	if cfg.InstanceId != "split-config-test" || cfg.Business != nil || cfg.GetVenueConnection().GetTarget() != "127.0.0.1:8080" {
		t.Fatal("split configuration lost service or infrastructure values")
	}
}
