package coreentry

import (
	"io"
	"testing"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

func TestCoreEntryRejectsManagementBeforeOpeningAnyDependency(t *testing.T) {
	for _, role := range []string{"admin", "api", "operator", "cron"} {
		if err := Run(t.Context(), role, &configv1.ServiceConfig{Name: role}, io.Discard); err == nil {
			t.Fatalf("core entry accepted %s", role)
		}
	}
	cfg := &configv1.ServiceConfig{
		Name: "strategy",
		Business: &configv1.BusinessRuntime{Strategy: &configv1.StrategyRuntime{
			ManagementEnabled: true,
		}},
	}
	if err := Run(t.Context(), "strategy", cfg, io.Discard); err == nil {
		t.Fatal("core entry allowed administration assembly")
	}
}
