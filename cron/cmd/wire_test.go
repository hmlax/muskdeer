package main

import (
	"context"
	"io"
	"testing"

	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/proto/common/config/v1"

	"github.com/hmlax/muskdeer/deer/testapp"
)

func TestServiceRejectsOtherIdentitiesAndAdministrationBeforeOpeningResources(t *testing.T) {
	for _, name := range []string{"cron", "strategy", "execution", "portfolio", "admin", "operator"} {
		if name == "cron" {
			continue
		}
		if _, err := wireApp(t.Context(), &configv1.ServiceConfig{Name: name}, io.Discard, nil); err == nil {
			t.Fatalf("cron accepted %s configuration", name)
		}
	}
	cfg := &configv1.ServiceConfig{
		Name:     "cron",
		Business: &configv1.BusinessRuntime{Strategy: &configv1.StrategyRuntime{ManagementEnabled: true}},
	}
	if _, err := wireApp(t.Context(), cfg, io.Discard, nil); err == nil {
		t.Fatal("cron allowed administration assembly")
	}
}

func TestWireAppRollsBackAndCanCloseBeforeRun(t *testing.T) {
	testapp.AssertLifecycle(t, "cron", func(ctx context.Context, cfg *configv1.ServiceConfig, output io.Writer, options []deer.Option) (testapp.Application, error) {
		application, err := wireApp(ctx, cfg, output, options)
		if err != nil {
			return nil, err
		}
		return application, nil
	})
}

func TestServiceProvidesGRPCHealth(t *testing.T) {
	testapp.AssertGRPCHealth(t, "cron", func(ctx context.Context, cfg *configv1.ServiceConfig, output io.Writer, options []deer.Option) (testapp.Application, error) {
		application, err := wireApp(ctx, cfg, output, options)
		if err != nil {
			return nil, err
		}
		return application, nil
	})
}
