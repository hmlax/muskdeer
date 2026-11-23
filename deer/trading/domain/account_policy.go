package domain

// Account policy preconditions are stable public business reasons.
const (
	AccountPolicyOrdersReason    = "ACCOUNT_POLICY_ORDER_LEVERAGE"
	AccountPolicyOrdersMessage   = "cancel current orders before changing account leverage"
	AccountPolicyConflictReason  = "ACCOUNT_POLICY_CONFLICT"
	AccountPolicyConflictMessage = "account parameters changed; refresh before editing again"
	AccountPolicyExposureReason  = "ACCOUNT_POLICY_EXPOSURE"
	AccountPolicyExposureMessage = "new total limit is below current matching exposure; cancel orders or reduce positions first"
	AccountPolicyPositionReason  = "ACCOUNT_POLICY_POSITION_LEVERAGE"
	AccountPolicyPositionMessage = "close current positions before changing account leverage"
	AccountPolicyGridReason      = "ACCOUNT_POLICY_GRID_LIMIT"
	AccountPolicyGridMessage     = "account limits must cover the configured long grid order and position budgets"
	AccountPolicySnapshotReason  = "ACCOUNT_POLICY_SNAPSHOT_UNAVAILABLE"
	AccountPolicySnapshotMessage = "a fresh matching account observation is required to update account parameters"
)

// AccountPolicyMessage admits only fixed public precondition messages.
func AccountPolicyMessage(reason string) (string, bool) {
	switch reason {
	case AccountPolicyOrdersReason:
		return AccountPolicyOrdersMessage, true
	case AccountPolicyConflictReason:
		return AccountPolicyConflictMessage, true
	case AccountPolicyExposureReason:
		return AccountPolicyExposureMessage, true
	case AccountPolicyPositionReason:
		return AccountPolicyPositionMessage, true
	case AccountPolicyGridReason:
		return AccountPolicyGridMessage, true
	case AccountPolicySnapshotReason:
		return AccountPolicySnapshotMessage, true
	default:
		return "", false
	}
}
