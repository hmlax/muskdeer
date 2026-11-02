// Command readycheck probes the configured service without starting another Deer.
package main

import (
	"context"
	"os"
	"runtime"
	"time"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/internal/coreentry"
	"github.com/hmlax/muskdeer/internal/processfailure"
)

func main() {
	if len(os.Args) != 1 || runtime.Version() != "go1.27.3" {
		os.Exit(processfailure.Report(os.Stderr, "readiness probe takes no arguments and requires Go 1.27.3", nil, 2))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	cfg, err := deer.LoadNacosConfig(ctx, "")
	if err == nil {
		err = coreentry.CheckReady(ctx, cfg.Monitoring)
	}
	cancel()
	if err != nil {
		os.Exit(processfailure.Report(os.Stderr, "readiness probe failed", err, 1))
	}
}
