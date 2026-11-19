package venue

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
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
)

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
