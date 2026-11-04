package deer

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"testing"
)

func TestNacosLoadsSeparateDataAndServiceYAML(t *testing.T) {
	data := []byte("name: strategy\nversion: test\nbusiness:\n  strategy:\n    account_ids: [account-from-data]\n")
	service := fmt.Sprintf("name: strategy\ninstance_id: split-config-test\nversion: test\nstartup_timeout: 5s\nshutdown_timeout: 5s\ngrpc:\n  address: '127.0.0.1:0'\n  request_timeout: 1s\ndata_sha256: '%x'\n", sha256.Sum256(data))
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
	t.Setenv("NACOS_DATA_ID", "")
	t.Setenv("NACOS_DATA_CONFIG_ID", "strategy.data.yaml")
	t.Setenv("NACOS_SERVICE_CONFIG_ID", "strategy.service.yaml")
	cfg, err := LoadNacosConfig(t.Context(), "strategy")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InstanceId != "split-config-test" || cfg.Business.Strategy.AccountIds[0] != "account-from-data" {
		t.Fatal("split configuration lost service or business values")
	}
}
