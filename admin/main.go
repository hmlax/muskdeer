package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	adminbootstrap "github.com/hmlax/muskdeer/admin/internal/bootstrap"
	"github.com/hmlax/muskdeer/deer/management/runtimeconfig"
	"github.com/hmlax/muskdeer/deer/management/serviceid"
)

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	boot, closeConfig, err := runtimeconfig.NewContext(ctx, serviceid.AdminService, Version)
	if err != nil {
		return err
	}
	defer closeConfig()
	servers, cleanup, err := adminbootstrap.New(boot, register)
	if err != nil {
		return err
	}
	defer cleanup()
	return adminbootstrap.Run(ctx, boot, servers)
}
func main() {
	slog.New(slog.NewJSONHandler(os.Stderr, nil)).Info("service.build", "name", Name, "version", Version, "build", Build, "instance", ID)
	if err := run(); err != nil {
		slog.Error("admin stopped", "error", err)
		os.Exit(1)
	}
}
