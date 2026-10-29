package strategywire

import (
	"strings"
	"unicode/utf8"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// RiskCommand maps explicit business fields without an opaque JSON bridge.
func RiskCommand(input *strategyv1.ChangeStrategyRiskInput, strategyID int64, key string) (domain.AdminRiskRequest, error) {
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 {
		return domain.AdminRiskRequest{}, domain.ErrInvalidAdminRisk
	}
	return (domain.AdminRiskRequest{
		Key:                   key,
		StrategyID:            strategyID,
		ExpectedRevision:      input.ExpectedRevision,
		ExpectedConfigVersion: input.ExpectedConfigVersion,
		ExpectedDesiredState:  domain.StrategyState(input.ExpectedDesiredState),
		Action:                domain.AdminRiskAction(input.Action),
		Reason:                input.Reason,
	}).Normalize()
}

// ValidRiskState rejects mismatched identities, unsafe amounts or unknown states.
func ValidRiskState(value *strategyv1.StrategyRiskState, strategyID int64) bool {
	if value == nil || len(value.ProtoReflect().GetUnknown()) != 0 || strategyID <= 0 || value.StrategyId != strategyID ||
		value.Revision <= 0 || value.RequestedConfigVersion <= 0 || value.UpdatedAtMs <= 0 ||
		!riskText(value.QuoteAsset, 32) || !riskActualState(domain.StrategyState(value.State)) ||
		!riskDesiredState(domain.StrategyState(value.DesiredState)) || domain.MarketSource(value.MarketDataSource).ValidateSelection() != nil {
		return false
	}
	limit, limitErr := domain.ParseLedgerDecimal(value.LimitNotional, false, false)
	reserved, reservedErr := domain.ParseLedgerDecimal(value.ReservedNotional, false, true)
	return limitErr == nil && reservedErr == nil && !reserved.GreaterThan(limit)
}

// ValidRiskReceipt binds an immutable response to the normalized original request.
func ValidRiskReceipt(value *strategyv1.StrategyRiskReceipt, request domain.AdminRiskRequest) bool {
	return value != nil && len(value.ProtoReflect().GetUnknown()) == 0 && value.Id > 0 && value.ActorUserId > 0 &&
		value.StrategyId == request.StrategyID && value.Action == string(request.Action) &&
		value.PreviousRevision == request.ExpectedRevision && value.Revision == request.ExpectedRevision+1 &&
		value.Enabled == (request.Action == domain.AdminRiskUnblock) &&
		value.ExpectedConfigVersion == request.ExpectedConfigVersion && value.ExpectedDesiredState == string(request.ExpectedDesiredState) &&
		value.Reason == request.Reason && value.CreatedAtMs > 0
}

func riskActualState(state domain.StrategyState) bool {
	switch state {
	case domain.StrategyStateCreated, domain.StrategyStateStarting, domain.StrategyStateRecovering, domain.StrategyStateRunning,
		domain.StrategyStatePausing, domain.StrategyStatePaused, domain.StrategyStateStopping, domain.StrategyStateStopped, domain.StrategyStateError:
		return true
	default:
		return false
	}
}

func riskDesiredState(state domain.StrategyState) bool {
	return state == domain.StrategyStateRunning || state == domain.StrategyStatePaused || state == domain.StrategyStateStopped
}

func riskText(value string, maximum int) bool {
	return len(value) > 0 && len(value) <= maximum && utf8.ValidString(value) && strings.TrimSpace(value) == value && !strings.ContainsRune(value, 0)
}
