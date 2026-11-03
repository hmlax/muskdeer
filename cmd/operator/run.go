package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/entry"
)

type commandFailure struct {
	Level      string `json:"level"`
	Message    string `json:"msg"`
	Cause      string `json:"cause,omitempty"`
	CauseKind  string `json:"cause_kind,omitempty"`
	CauseStage string `json:"cause_stage,omitempty"`
}

const (
	commandCauseDeadline     = "deadline_exceeded"
	commandCauseCanceled     = "canceled"
	commandCauseLeaseLost    = "lease_lost"
	commandCauseConflict     = "idempotency_conflict"
	commandCauseRiskDenied   = "risk_denied"
	commandCauseInvalidOrder = "invalid_order"
)

func run(ctx context.Context, args []string, output, diagnostic io.Writer) int {
	const role = entry.RoleOperator
	flags := flag.NewFlagSet(string(role), flag.ContinueOnError)
	flags.SetOutput(io.Discard)
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
	if role != entry.RoleOperator {
		return report(diagnostic, "unsupported application", nil, 2)
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
	var detail, kind, stage string
	if cause != nil {
		// Library errors mask credentials; config errors mask original input.
		detail = cause.Error()
		switch {
		case errors.Is(cause, context.DeadlineExceeded):
			kind = commandCauseDeadline
		case errors.Is(cause, context.Canceled):
			kind = commandCauseCanceled
		case errors.Is(cause, domain.ErrLeaseLost):
			kind = commandCauseLeaseLost
		case errors.Is(cause, domain.ErrIdempotencyConflict):
			kind = commandCauseConflict
		case errors.Is(cause, domain.ErrRiskDenied):
			kind = commandCauseRiskDenied
		case errors.Is(cause, domain.ErrInvalidOrder):
			kind = commandCauseInvalidOrder
		}
		var failure *domain.StrategyFailure
		if errors.As(cause, &failure) {
			stage = failure.SafeStage()
		}
	}
	if err := json.NewEncoder(output).Encode(commandFailure{
		Level:      "ERROR",
		Message:    message,
		Cause:      detail,
		CauseKind:  kind,
		CauseStage: stage,
	}); err != nil {
		return 1
	}
	return code
}
