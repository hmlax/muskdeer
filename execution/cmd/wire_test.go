package main

import (
	"context"
	"io"
	"testing"

	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"

	"github.com/hmlax/muskdeer/deer/testapp"
)

func TestServiceRejectsOtherIdentitiesAndAdministrationBeforeOpeningResources(t *testing.T) {
	for _, name := range []string{"market", "strategy", "execution", "portfolio", "admin", "operator"} {
		if name == "execution" {
			continue
		}
		if _, err := wireApp(t.Context(), &configv1.ServiceConfig{Name: name}, io.Discard, nil); err == nil {
			t.Fatalf("execution accepted %s configuration", name)
		}
	}
	cfg := &configv1.ServiceConfig{
		Name:     "execution",
		Business: &configv1.BusinessRuntime{Strategy: &configv1.StrategyRuntime{ManagementEnabled: true}},
	}
	if _, err := wireApp(t.Context(), cfg, io.Discard, nil); err == nil {
		t.Fatal("execution allowed administration assembly")
	}
}

func TestWireAppRollsBackAndCanCloseBeforeRun(t *testing.T) {
	testapp.AssertLifecycle(t, "execution", func(ctx context.Context, cfg *configv1.ServiceConfig, output io.Writer, options []deer.Option) (testapp.Application, error) {
		application, err := wireApp(ctx, cfg, output, options)
		if err != nil {
			return nil, err
		}
		return application, nil
	})
}

func TestServiceProvidesGRPCHealth(t *testing.T) {
	testapp.AssertGRPCHealth(t, "execution", func(ctx context.Context, cfg *configv1.ServiceConfig, output io.Writer, options []deer.Option) (testapp.Application, error) {
		application, err := wireApp(ctx, cfg, output, options)
		if err != nil {
			return nil, err
		}
		return application, nil
	})
}
