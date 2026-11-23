package domain

// Maker check reasons are shared by console, operator and browser contracts.
const (
	MakerReadyReason         = "MAKER_CHECK_PASSED"
	MakerBudgetReason        = "MAKER_BUDGET_INSUFFICIENT"
	MakerSnapshotReason      = "MAKER_SNAPSHOT_UNAVAILABLE"
	MakerSourceReason        = "MAKER_SOURCE_UNAVAILABLE"
	MakerTargetsReason       = "MAKER_TARGETS_INVALID"
	MakerRevisionReason      = "MAKER_REVISION_NOT_LOADED"
	MakerCheckExpiredReason  = "MAKER_CHECK_EXPIRED"
	MakerReadyMessage        = "current quotation inputs and total notional check passed; execution risk checks still apply"
	MakerBudgetMessage       = "target quotation notional exceeds the account limit; review the quotation check before starting"
	MakerSnapshotMessage     = "a fresh matching account observation is required"
	MakerSourceMessage       = "a fresh two-sided selected-source order book is required"
	MakerTargetsMessage      = "quotation parameters do not match current market rules"
	MakerRevisionMessage     = "restart core services to load this instance revision before starting"
	MakerCheckExpiredMessage = "quotation check expired; refresh before starting"
)

// StartPreconditionMessage returns only safe, fixed public messages.
func StartPreconditionMessage(reason string) (string, bool) {
	switch reason {
	case AccountDisabledReason:
		return AccountDisabledMessage, true
	case MakerBudgetReason:
		return MakerBudgetMessage, true
	case MakerSnapshotReason:
		return MakerSnapshotMessage, true
	case MakerSourceReason:
		return MakerSourceMessage, true
	case MakerTargetsReason:
		return MakerTargetsMessage, true
	case MakerRevisionReason:
		return MakerRevisionMessage, true
	case MakerCheckExpiredReason:
		return MakerCheckExpiredMessage, true
	default:
		return "", false
	}
}
