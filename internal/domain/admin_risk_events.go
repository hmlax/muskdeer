package domain

import (
	"context"
	"time"
)

// AdminRiskEventFilter selects recorded history without requiring its source
// snapshot, strategy or account to remain active. Zero fields are optional.
type AdminRiskEventFilter struct {
	Page         AdminPage
	StrategyID   int64
	AccountID    string
	InstrumentID int64
	Source       MarketSource
	Kind         RiskSnapshotEventKind
	Reason       RiskReason
}

// Validate requires bounded pagination and exact, known filter values.
func (f AdminRiskEventFilter) Validate() error {
	if f.Page.AfterID < 0 || f.Page.Limit < 1 || f.Page.Limit > MaxAdminPageSize || f.StrategyID < 0 || f.InstrumentID < 0 ||
		f.AccountID != "" && !intentIdentityPart(f.AccountID, 64) ||
		f.Source != MarketSourceUnspecified && f.Source.ValidateSelection() != nil ||
		f.Kind != RiskSnapshotEventUnspecified && !ValidRiskSnapshotEventKind(f.Kind) ||
		f.Reason != RiskReasonUnspecified && !ValidRiskReason(f.Reason) {
		return ErrInvalidAdminQuery
	}
	return nil
}

// AdminRiskEvent retains original recorded inputs. These flags and times never
// certify present reconciliation, fresh market data or permission to trade.
type AdminRiskEvent struct {
	ID                   int64
	RecordedAt           time.Time
	Kind                 RiskSnapshotEventKind
	SnapshotID           int64
	SnapshotRevision     int64
	StrategyID           int64
	AccountID            string
	InstrumentID         int64
	ConfigVersion        int64
	Source               MarketSource
	FeedID               *int64
	PublisherEpoch       int64
	MarketSequence       int64
	PublicationRevision  int64
	SourceVersion        int64
	MarketMaximumAge     time.Duration
	ObservedAt           time.Time
	ExpiresAt            time.Time
	RecordedReconciled   bool
	RecordedAllowNewRisk bool
	Reason               RiskReason
	SnapshotDeleted      bool
}

// AdminRiskEventPage orders immutable recording identities, not source revisions.
// Pagination is for interactive history reads, not a durable delivery cursor.
type AdminRiskEventPage struct {
	Items       []AdminRiskEvent
	NextAfterID int64
}

// AdminRiskEvents authenticates current viewer authority in a read-only snapshot.
// Reads do not evaluate risk, rebuild positions, initialize funds or write state.
type AdminRiskEvents interface {
	List(context.Context, string, AdminRiskEventFilter) (AdminRiskEventPage, error)
	Detail(context.Context, string, int64) (AdminRiskEvent, error)
}
