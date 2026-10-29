package domain

import (
	"context"
	"time"
)

// RiskReason is a persisted, bounded eligibility diagnostic, never a raw error.
type RiskReason string

// Persisted eligibility reasons do not replace per-order authorization.
const (
	RiskReasonNotEvaluated         RiskReason = "NOT_EVALUATED"
	RiskReasonReady                RiskReason = "READY"
	RiskReasonStopped              RiskReason = "STRATEGY_STOPPED"
	RiskReasonParameters           RiskReason = "INVALID_PARAMETERS"
	RiskReasonBudget               RiskReason = "BUDGET_DISABLED"
	RiskReasonUnreconciled         RiskReason = "UNRECONCILED"
	RiskReasonMarket               RiskReason = "MARKET_UNAVAILABLE"
	RiskReasonConfigurationPending RiskReason = "CONFIGURATION_PENDING"
)

// RiskDecision is eligibility only; every order still needs atomic limits,
// funds and final strategy/execution/market fencing at its actual target.
type RiskDecision struct {
	StrategyID int64
	Allowed    bool
	Reconciled bool
	Reason     RiskReason
	ObservedAt time.Time
	ExpiresAt  time.Time
	Market     MarketReference
}

// RiskEvaluator reads authoritative current inputs and persists one decision.
// It does not initialize balances/projections, enable budgets or fix mismatches.
type RiskEvaluator interface {
	Evaluate(ctx context.Context, strategyID int64) (RiskDecision, error)
}

// RiskScope identifies cross-account quote-notional budgets.
type RiskScope string

// Global scopes retain independent units for each quote asset.
const (
	RiskScopePlatform   RiskScope = "PLATFORM"
	RiskScopeInstrument RiskScope = "INSTRUMENT"
)

// MaxAccountInventoryPolicies bounds account-wide configuration work.
const MaxAccountInventoryPolicies = 128

// MaxRunningRiskCandidates bounds one account evaluation cycle.
const MaxRunningRiskCandidates = 16

// RiskCandidates reads currently desired instances within an explicit account.
type RiskCandidates interface {
	DesiredForAccount(ctx context.Context, accountID string) ([]int64, error)
}
