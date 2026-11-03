// Command portfolio runs one independent Kratos core service.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/internal/coreentry"
	"github.com/hmlax/muskdeer/internal/processfailure"
)

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) != 1 {
		return processfailure.Report(os.Stderr, "core service takes no arguments", nil, 2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := deer.LoadNacosConfig(ctx, "portfolio")
	if err != nil {
		return exitCode(err)
	}
	application, err := wireApp(ctx, cfg, os.Stdout, nil)
	if err != nil {
		return exitCode(err)
	}
	return exitCode(application.Run(ctx))
}

func exitCode(err error) int {
	if err != nil && !coreentry.IsCancellation(err) {
		return processfailure.Report(os.Stderr, "core service failed", err, 1)
	}
	return 0
}
