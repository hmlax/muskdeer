// Command portfolio is an independently built regression fixture, not a deployed application.
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/processfailure"
	"github.com/hmlax/muskdeer/deer/testentry"
)

func main() {
	path := flag.String("config", "", "module fixture configuration")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	cfg, err := deer.LoadConfig(ctx, *path)
	if err == nil {
		err = testentry.Run(ctx, testentry.RolePortfolio, cfg, os.Stdout)
	}
	stop()
	if err != nil && !testentry.IsCancellation(err) {
		code := processfailure.Report(os.Stderr, "service failed", err, 1)
		os.Exit(code)
	}
}
