package deer

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestBuildInfoDoesNotReplaceConfigurationIdentity(t *testing.T) {
	var output bytes.Buffer
	cfg := serviceConfig("configured-instance")
	owner, cleanup, err := NewDeer(t.Context(), cfg, ServiceSpec{LogOutput: &output}, WithBuildInfo(BuildInfo{
		Name:    "compiled-name",
		Version: "v1.2.3",
		Build:   "abc123@2026-11-19T00:00:00Z",
		ID:      "runtime-host",
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cleanup(t.Context()); err != nil {
			t.Error(err)
		}
	}()
	var record struct {
		Message  string `json:"msg"`
		Service  string `json:"service"`
		Instance string `json:"instance"`
		Name     string `json:"software_name"`
		Version  string `json:"software_version"`
		Build    string `json:"software_build"`
		ID       string `json:"node_id"`
	}
	if err := json.NewDecoder(&output).Decode(&record); err != nil {
		t.Fatal(err)
	}
	if record.Message != "build.info" || record.Name != "compiled-name" || record.Version != "v1.2.3" || record.Build != "abc123@2026-11-19T00:00:00Z" || record.ID != "runtime-host" {
		t.Fatalf("missing compiled metadata: %+v", record)
	}
	if record.Service != cfg.Name || record.Instance != cfg.InstanceId || owner.cfg.Version != cfg.Version || owner.cfg.Name != cfg.Name || owner.cfg.InstanceId != cfg.InstanceId {
		t.Fatal("compiled metadata changed configured identity or pairing version")
	}
}
