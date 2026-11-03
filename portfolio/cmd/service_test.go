package main

import (
	"io"
	"testing"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

func TestServiceRejectsOtherIdentitiesAndAdministrationBeforeOpeningResources(t *testing.T) {
	for _, name := range []string{"market", "strategy", "execution", "portfolio", "admin", "operator"} {
		if name == "portfolio" {
			continue
		}
		if err := runService(t.Context(), &configv1.ServiceConfig{Name: name}, io.Discard); err == nil {
			t.Fatalf("portfolio accepted %s configuration", name)
		}
	}
	cfg := &configv1.ServiceConfig{
		Name:     "portfolio",
		Business: &configv1.BusinessRuntime{Strategy: &configv1.StrategyRuntime{ManagementEnabled: true}},
	}
	if err := runService(t.Context(), cfg, io.Discard); err == nil {
		t.Fatal("portfolio allowed administration assembly")
	}
}
