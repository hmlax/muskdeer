package domain

import "time"

// AdminFlattenTaskState describes durable work, separately from confirmation.
type AdminFlattenTaskState string

// Both stages remain pending; neither grants trading nor proves zero inventory.
const (
	AdminFlattenQueued AdminFlattenTaskState = "QUEUED"
	AdminFlattenActive AdminFlattenTaskState = "ACTIVE"
)

// AdminFlattenProgress preserves the last stored claim. It does not establish
// that the owner still holds a lease when the observation reaches a reader.
type AdminFlattenProgress struct {
	ID           int64
	FlatteningID int64
	StrategyID   int64
	AccountID    string
	InstrumentID int64
	State        AdminFlattenTaskState
	Revision     int64
	OwnerID      string
	OwnerEpoch   int64
	ClaimedAt    time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
