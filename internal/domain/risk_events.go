package domain

// RiskSnapshotEventKind describes an immutable diagnostic recording, not an
// authorization transition or an automatically reconciled account.
type RiskSnapshotEventKind string

// A baseline starts available history without inventing earlier transitions.
const (
	// RiskSnapshotEventUnspecified is a query wildcard, never persisted.
	RiskSnapshotEventUnspecified RiskSnapshotEventKind = ""
	RiskSnapshotEventBaseline    RiskSnapshotEventKind = "BASELINE"
	RiskSnapshotEventChanged     RiskSnapshotEventKind = "CHANGED"
	RiskSnapshotEventRemoved     RiskSnapshotEventKind = "REMOVED"
)

// ValidRiskSnapshotEventKind accepts only defined persisted recording kinds.
func ValidRiskSnapshotEventKind(kind RiskSnapshotEventKind) bool {
	switch kind {
	case RiskSnapshotEventBaseline, RiskSnapshotEventChanged, RiskSnapshotEventRemoved:
		return true
	default:
		return false
	}
}
