package venue

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/tracing"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

const (
	quoteStepSource           = "source"
	quoteStepObservation      = "observation"
	quoteStepTargets          = "targets"
	quoteStepRoom             = "room"
	quoteStepFill             = "fill"
	quoteStepRetirement       = "retirement"
	quoteStepProtection       = "protection"
	reasonQuoteTargetExpired  = "QUOTE_TARGET_EXPIRED"
	reasonQuoteSideExhaustion = "QUOTE_SIDE_EXHAUSTION"
	reasonInvalidSubmit       = "INVALID_SUBMIT"
	reasonInvalidConfig       = "INVALID_CONFIG"
	reasonNotDispatched       = "NOT_DISPATCHED"
	reasonOutcomeUnknown      = "OUTCOME_UNKNOWN"
	quoteOperationPlace       = "place"
	quoteOperationCancel      = "cancel"
)

// Trace only the wait for the existing process-wide send budget. It ends before
// configuration checks or SDK dispatch and never changes admission or deadlines.
func (r *Runtime) traceQuoteAdmission(ctx context.Context, operation string) func(error) {
	work, span := tracing.StartChild(ctx, "execution.quote.admission")
	if !span.IsRecording() {
		return func(error) { span.End() }
	}
	started := time.Now()
	usage := func() (int, int) {
		if r.quoteBudget == nil {
			return len(r.quoteSlots), cap(r.quoteSlots)
		}
		r.quoteBudget.mu.Lock()
		defer r.quoteBudget.mu.Unlock()
		return r.quoteBudget.active, r.quoteBudget.limit
	}
	active, capacity := usage()
	span.SetAttributes(attribute.String("quote.operation", operation),
		attribute.Int("quote.admission.in_use", active), attribute.Int("quote.admission.capacity", capacity),
		attribute.Bool("quote.owner.reserved", quoteSendPermitFrom(ctx) != nil))
	return func(err error) {
		active, _ := usage()
		span.SetAttributes(attribute.Int64("quote.admission.wait_ms", time.Since(started).Milliseconds()),
			attribute.Bool("quote.admission.admitted", err == nil), attribute.Int("quote.admission.in_use_after", active),
			attribute.String("quote.reason", safeReason(err)))
		if deadline, ok := ctx.Deadline(); ok {
			span.SetAttributes(attribute.Int64("quote.operation_left_ms", time.Until(deadline).Milliseconds()))
		}
		tracing.RecordError(work, err)
		span.End()
	}
}

type quoteStepError struct {
	step  string
	cause error
}

func (e *quoteStepError) Error() string { return e.step + ": " + e.cause.Error() }
func (e *quoteStepError) Unwrap() error { return e.cause }

func quoteFailed(step string, err error) error {
	if err == nil {
		return nil
	}
	return &quoteStepError{
		step:  step,
		cause: err,
	}
}

func quoteFailureStep(err error) string {
	var failure *quoteStepError
	if errors.As(err, &failure) {
		return failure.step
	}
	return ""
}

// Report a real impending loss of own liquidity without changing selection,
// retaining an unsafe quote, or including SDK payloads or financial identities.
func (r *Runtime) warnQuoteDrain(ctx context.Context, cycle *quoteCycle, selected []*common.VenueOrder) error {
	active, removed := make(map[domain.Side]int), make(map[domain.Side]int)
	prefix := native.InstanceClientPrefix(cycle.profile)
	for _, order := range cycle.snapshot.Orders {
		if native.Active(order.Status) && strings.HasPrefix(order.ClientId, prefix) {
			active[domain.Side(order.Side)]++
		}
	}
	for _, order := range selected {
		removed[domain.Side(order.Side)]++
	}
	for side, count := range active {
		if count == 0 || removed[side] < count {
			continue
		}
		now := time.Now()
		deadline, _ := ctx.Deadline()
		if err := r.logger.LogAttrs(ctx, slog.LevelWarn, native.StateRecovering,
			slog.String("symbol", cycle.profile.Symbol), slog.String("reason", reasonQuoteSideExhaustion), slog.String("side", string(side)),
			slog.Int("active", count), slog.Int("selected", removed[side]), slog.Uint64("placed", uint64(cycle.result.Placed)),
			slog.Int64("source_left_ms", cycle.authority.Book.ValidUntilMs-now.UnixMilli()),
			slog.Int64("snapshot_left_ms", cycle.snapshot.ValidUntilMs-now.UnixMilli()),
			slog.Int64("operation_left_ms", deadline.Sub(now).Milliseconds())); err != nil {
			return err
		}
	}
	return nil
}
