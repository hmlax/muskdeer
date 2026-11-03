package main

import (
	"io"
	"testing"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

func TestServiceRejectsOtherIdentitiesAndAdministrationBeforeOpeningResources(t *testing.T) {
	for _, name := range []string{"market", "strategy", "execution", "portfolio", "admin", "operator"} {
		if name == "execution" {
			continue
		}
		if err := runService(t.Context(), &configv1.ServiceConfig{Name: name}, io.Discard); err == nil {
			t.Fatalf("execution accepted %s configuration", name)
		}
	}
	cfg := &configv1.ServiceConfig{
		Name:     "execution",
		Business: &configv1.BusinessRuntime{Strategy: &configv1.StrategyRuntime{ManagementEnabled: true}},
	}
	if err := runService(t.Context(), cfg, io.Discard); err == nil {
		t.Fatal("execution allowed administration assembly")
	}
}
