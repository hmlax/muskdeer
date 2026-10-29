package domain

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

// AdminRiskAction changes one strategy's eligibility, never its runtime target.
type AdminRiskAction string

// Risk control names and fixed public failures are shared by all boundaries.
const (
	AdminRiskBlock                 AdminRiskAction = "risk.strategy.block"
	AdminRiskUnblock               AdminRiskAction = "risk.strategy.unblock"
	AdminAuditResourceRisk                         = "strategy_risk"
	MessageInvalidAdminRisk                        = "invalid strategy risk control request"
	MessageAdminRiskRecoveryDenied                 = "strategy risk recovery conditions are not met"
)

// Risk control failures contain no persistence or submitted request details.
var (
	ErrInvalidAdminRisk        = errors.New(MessageInvalidAdminRisk)
	ErrAdminRiskRecoveryDenied = errors.New(MessageAdminRiskRecoveryDenied)
)

// AdminRiskRequest uses a revision independent of financial reservation updates.
type AdminRiskRequest struct {
	Key              string
	StrategyID       int64
	ExpectedRevision int64
	// Unblock also binds the configuration and target shown at confirmation.
	// Block is safe for every target and leaves these two fields empty.
	ExpectedConfigVersion int64
	ExpectedDesiredState  StrategyState
	Action                AdminRiskAction
	Reason                string
}

// Normalize bounds identity and requires an explicit block/unblock action.
func (r AdminRiskRequest) Normalize() (AdminRiskRequest, error) {
	r.Reason = strings.TrimSpace(r.Reason)
	if r.StrategyID <= 0 || r.ExpectedRevision <= 0 || r.ExpectedRevision == math.MaxInt64 ||
		len(r.Key) < 16 || !intentIdentityPart(r.Key, 64) ||
		(r.Action != AdminRiskBlock && r.Action != AdminRiskUnblock) ||
		len(r.Reason) == 0 || len(r.Reason) > 256 || !utf8.ValidString(r.Reason) || strings.ContainsRune(r.Reason, 0) {
		return AdminRiskRequest{}, ErrInvalidAdminRisk
	}
	if r.Action == AdminRiskBlock {
		if r.ExpectedConfigVersion != 0 || r.ExpectedDesiredState != "" {
			return AdminRiskRequest{}, ErrInvalidAdminRisk
		}
		return r, nil
	}
	if r.ExpectedConfigVersion <= 0 || (r.ExpectedDesiredState != StrategyStateRunning && r.ExpectedDesiredState != StrategyStatePaused && r.ExpectedDesiredState != StrategyStateStopped) {
		return AdminRiskRequest{}, ErrInvalidAdminRisk
	}
	return r, nil
}

// AdminRiskReceipt proves a historical flag change, not canceled orders or a
// risk grant. A later read may observe another request and runtime target.
type AdminRiskReceipt struct {
	ID                    int64
	ActorUserID           int64
	StrategyID            int64
	Action                AdminRiskAction
	PreviousEnabled       bool
	Enabled               bool
	PreviousRevision      int64
	Revision              int64
	ExpectedConfigVersion int64
	ExpectedDesiredState  StrategyState
	Reason                string
	CreatedAt             time.Time
}

// AdminRiskState is one current-viewer snapshot. Reserved notional stays held
// until the existing execution/core protocol confirms terminal obligations.
type AdminRiskState struct {
	StrategyID             int64
	Enabled                bool
	Revision               int64
	QuoteAsset             string
	LimitNotional          decimal.Decimal
	ReservedNotional       decimal.Decimal
	State                  StrategyState
	DesiredState           StrategyState
	RequestedConfigVersion int64
	Source                 MarketSource
	UpdatedAt              time.Time
}

// AdminStrategyRisk requires current operator authority for block and current
// admin authority plus fresh recovery checks for unblock, inside its transaction.
type AdminStrategyRisk interface {
	Change(context.Context, string, string, AdminRiskRequest) (AdminRiskReceipt, error)
	State(context.Context, string, int64) (AdminRiskState, error)
}
