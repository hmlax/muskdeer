package domain

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// Creation identifiers and fixed public failures.
const (
	AdminStrategyCreate         = "strategy.create"
	MessageInvalidAdminCreation = "invalid strategy creation request or provisioned dependency"
)

// ErrInvalidAdminCreation never includes a submitted key, reason or dependency.
var ErrInvalidAdminCreation = errors.New(MessageInvalidAdminCreation)

// AdminCreateStrategy explicitly binds immutable identity and initial parameters.
// Creation does not request running or initialize any account funds.
type AdminCreateStrategy struct {
	Key          string
	InstanceKey  string
	AccountID    string
	InstrumentID int64
	CodeVersion  string
	Source       MarketSource
	Mode         MarketMode
	Parameters   StrategyParameters
	Reason       string
}

// Normalize validates field identities and the concrete parameter union.
func (r AdminCreateStrategy) Normalize() (AdminCreateStrategy, error) {
	r.Reason = strings.TrimSpace(r.Reason)
	if len(r.Key) < 16 || !intentIdentityPart(r.Key, 64) || !intentIdentityPart(r.InstanceKey, 64) ||
		!intentIdentityPart(r.AccountID, 64) || !intentIdentityPart(r.CodeVersion, 64) || r.InstrumentID <= 0 ||
		r.Source.ValidateSelection() != nil || (r.Mode != MarketModeLive && r.Mode != MarketModeReplay) ||
		!utf8.ValidString(r.Reason) || len(r.Reason) == 0 || len(r.Reason) > 256 || strings.ContainsRune(r.Reason, 0) {
		return AdminCreateStrategy{}, ErrInvalidAdminCreation
	}
	switch r.Parameters.Template {
	case StrategyTemplateMarketMaker:
		if r.Parameters.Maker == nil || r.Parameters.Grid != nil || !boundedCreationParameters(r.Parameters) || r.Parameters.Maker.HedgeEnabled || r.Parameters.Maker.Validate() != nil {
			return AdminCreateStrategy{}, ErrInvalidAdminCreation
		}
	case StrategyTemplateGrid:
		if r.Parameters.Grid == nil || r.Parameters.Maker != nil || !boundedCreationParameters(r.Parameters) || r.Parameters.Grid.Validate() != nil {
			return AdminCreateStrategy{}, ErrInvalidAdminCreation
		}
	default:
		return AdminCreateStrategy{}, ErrInvalidAdminCreation
	}
	return r, nil
}

func boundedCreationParameters(p StrategyParameters) bool {
	if p.Grid != nil {
		g := p.Grid
		return FixedDecimal(g.LowerPrice, 20, 18, true) && FixedDecimal(g.UpperPrice, 20, 18, true) &&
			FixedDecimal(g.TotalBudget, 20, 18, true) && FixedDecimal(g.PerOrderNotional, 20, 18, true) && FixedDecimal(g.InventoryLimit, 20, 18, true)
	}
	m := p.Maker
	return FixedDecimal(m.LevelSpacingBPS, 20, 18, true) && FixedDecimal(m.BaseSpreadBPS, 20, 18, true) &&
		FixedDecimal(m.LevelNotional, 20, 18, true) && FixedDecimal(m.SideBudget, 20, 18, true) &&
		FixedDecimal(m.InventoryTarget, 20, 18, true) && FixedDecimal(m.InventorySoftLimit, 20, 18, true) && FixedDecimal(m.InventoryHardLimit, 20, 18, true) &&
		FixedDecimal(m.RepriceBPS, 20, 18, true) && FixedDecimal(m.HedgeThreshold, 20, 18, true) && FixedDecimal(m.MaxHedgeBacklog, 20, 18, true)
}

// AdminCreationReceipt proves metadata creation, independently of current runtime
// state. A replay returns this original receipt even after subsequent controls.
type AdminCreationReceipt struct {
	ID            int64
	ActorUserID   int64
	StrategyID    int64
	ConfigVersion int64
	Revision      int64
	Source        MarketSource
	Reason        string
	CreatedAt     time.Time
}

// AdminStrategyCreator checks operator/session/CSRF in the creation transaction.
type AdminStrategyCreator interface {
	Create(context.Context, string, string, AdminCreateStrategy) (AdminCreationReceipt, error)
}
