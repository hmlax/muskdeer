// Command portfolio is an independently built regression fixture, not a deployed application.
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/coreentry"
	"github.com/hmlax/muskdeer/deer/processfailure"
	configv1 "github.com/hmlax/muskdeer/deer/proto/common/config/v1"
	"github.com/hmlax/muskdeer/deer/testprocess"
	business "github.com/hmlax/muskdeer/portfolio/business"
)

func main() {
	path := flag.String("config", "", "module fixture configuration")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	cfg, err := deer.LoadConfig(ctx, *path)
	if err == nil {
		err = testprocess.RunFixture(ctx, "portfolio", cfg, os.Stdout, func(ctx context.Context, cfg *configv1.ServiceConfig, owner *deer.Deer) (testprocess.Runtime, error) {
			return business.NewPortfolio(ctx, cfg, owner)
		})
	}
	stop()
	if err != nil && !coreentry.IsCancellation(err) {
		code := processfailure.Report(os.Stderr, "service failed", err, 1)
		os.Exit(code)
	}
}
