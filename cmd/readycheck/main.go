// Command readycheck probes the configured service without starting another Deer.
package main

import (
	"context"
	"os"
	"time"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/coreentry"
	"github.com/hmlax/muskdeer/deer/processfailure"
)

func main() {
	if len(os.Args) != 1 {
		os.Exit(processfailure.Report(os.Stderr, "readiness probe takes no arguments", nil, 2))
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
