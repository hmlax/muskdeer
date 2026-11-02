// Command strategy runs one independent Kratos core service.
package main

import (
	"context"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/internal/coreentry"
	"github.com/hmlax/muskdeer/internal/processfailure"
)

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) != 1 || runtime.Version() != "go1.27.3" {
		return processfailure.Report(os.Stderr, "core service takes no arguments and requires Go 1.27.3", nil, 2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := deer.LoadNacosConfig(ctx, "strategy")
	if err == nil {
		err = runService(ctx, cfg, os.Stdout)
	}
	if err != nil && !coreentry.IsCancellation(err) {
		return processfailure.Report(os.Stderr, "core service failed", err, 1)
	}
	return 0
}
