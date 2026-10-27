// Command muskdeer runs one independent, configured Deer service process.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/internal/entry"
)

type commandFailure struct {
	Level     string `json:"level"`
	Message   string `json:"msg"`
	Cause     string `json:"cause,omitempty"`
	CauseKind string `json:"cause_kind,omitempty"`
}

const (
	commandCauseDeadline = "deadline_exceeded"
	commandCauseCanceled = "canceled"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, output, diagnostic io.Writer) int {
	flags := flag.NewFlagSet("muskdeer", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	roleName := flags.String("role", "", "api, market, strategy, execution, portfolio or cron")
	path := flags.String("config", "", "local protobuf JSON configuration")
	checkReady := flags.Bool("check-ready", false, "probe the configured local readiness endpoint and exit")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flags.SetOutput(output)
			flags.PrintDefaults()
			return 0
		}
		return report(diagnostic, "invalid command arguments", nil, 2)
	}
	if *path == "" || flags.NArg() != 0 {
		return report(diagnostic, "config is required and positional arguments are not accepted", nil, 2)
	}
	var role entry.Role
	if *checkReady {
		if *roleName != "" {
			return report(diagnostic, "readiness probe does not accept a service role", nil, 2)
		}
	} else {
		var err error
		role, err = entry.ParseRole(*roleName)
		if err != nil {
			return report(diagnostic, "invalid service role", nil, 2)
		}
	}
	if runtime.Version() != "go1.27.3" {
		return report(diagnostic, "Go 1.27.3 is required", nil, 1)
	}
	cfg, err := deer.LoadConfig(ctx, *path)
	if err != nil {
		return report(diagnostic, "configuration loading failed", err, 1)
	}
	if *checkReady {
		if err := entry.CheckReady(ctx, cfg.Monitoring); err != nil {
			return report(diagnostic, "readiness probe failed", err, 1)
		}
		return 0
	}
	if err := entry.Run(ctx, role, cfg, output); err != nil {
		if onlyCancellation(err) {
			return 0
		}
		return report(diagnostic, "service failed", err, 1)
	}
	return 0
}

func onlyCancellation(err error) bool {
	if err == context.Canceled {
		return true
	}
	if group, ok := err.(interface{ Unwrap() []error }); ok {
		children := group.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !onlyCancellation(child) {
				return false
			}
		}
		return true
	}
	if wrapper, ok := err.(interface{ Unwrap() error }); ok {
		return onlyCancellation(wrapper.Unwrap())
	}
	return false
}

func report(output io.Writer, message string, cause error, code int) int {
	var detail, kind string
	if cause != nil {
		// Library errors mask credentials; config errors mask original input.
		detail = cause.Error()
		switch {
		case errors.Is(cause, context.DeadlineExceeded):
			kind = commandCauseDeadline
		case errors.Is(cause, context.Canceled):
			kind = commandCauseCanceled
		}
	}
	if err := json.NewEncoder(output).Encode(commandFailure{
		Level:     "ERROR",
		Message:   message,
		Cause:     detail,
		CauseKind: kind,
	}); err != nil {
		return 1
	}
	return code
}
