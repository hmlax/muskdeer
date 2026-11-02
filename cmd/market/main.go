// Command market runs one independent Kratos core service.
package main

import (
	"context"
	"errors"
	"flag"
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
	flags := flag.NewFlagSet("market", flag.ContinueOnError)
	path := flags.String("config", "", "local protobuf JSON configuration")
	ready := flags.Bool("check-ready", false, "probe the local readiness endpoint and exit")
	if err := flags.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *path == "" || flags.NArg() != 0 || runtime.Version() != "go1.27.3" {
		return processfailure.Report(os.Stderr, "config and exact Go 1.27.3 are required", nil, 2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := deer.LoadConfig(ctx, *path)
	if err == nil {
		if *ready {
			err = coreentry.CheckReady(ctx, cfg.Monitoring)
		} else {
			err = coreentry.Run(ctx, "market", cfg, os.Stdout)
		}
	}
	if err != nil && !coreentry.IsCancellation(err) {
		return processfailure.Report(os.Stderr, "core service failed", err, 1)
	}
	return 0
}
