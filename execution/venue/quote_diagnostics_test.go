package venue

import (
	"errors"
	"testing"

	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func TestQuoteDiagnosticsPreserveUnknownAndWrappedErrorIdentity(t *testing.T) {
	t.Parallel()
	err := errors.Join(quoteFailed(quoteStepFill, ErrInvalidSubmit), quoteFailed(quoteStepProtection, domain.ErrOutcomeUnknown))
	if !errors.Is(err, ErrInvalidSubmit) || !errors.Is(err, domain.ErrOutcomeUnknown) || quoteFailureStep(err) != quoteStepFill || safeReason(err) != reasonOutcomeUnknown {
		t.Fatal("diagnostic wrapping changed error identity, origin, or unknown priority")
	}
	if safeReason(quoteFailed(quoteStepFill, ErrInvalidSubmit)) != reasonInvalidSubmit || safeReason(quoteFailed(quoteStepRoom, ErrInvalidConfig)) != reasonInvalidConfig || quoteFailed(quoteStepFill, nil) != nil {
		t.Fatal("known local failure was flattened into an unknown status")
	}
}
