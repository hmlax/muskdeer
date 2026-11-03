// Package processfailure preserves typed diagnostics for process boundaries.
package processfailure

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/hmlax/muskdeer/deer/trading/domain"
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

// Report writes one bounded, structured process failure.
func Report(output io.Writer, message string, cause error, code int) int {
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
