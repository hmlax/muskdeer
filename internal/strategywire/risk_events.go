package strategywire

import (
	"time"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// RiskEventProto preserves recorded diagnostics without re-evaluating expiry.
func RiskEventProto(value domain.AdminRiskEvent) (*strategyv1.RiskDiagnosticEvent, error) {
	if value.MarketMaximumAge%time.Millisecond != 0 || !riskEventMillis(value.RecordedAt) || !riskEventMillis(value.ObservedAt) || !riskEventMillis(value.ExpiresAt) {
		return nil, domain.ErrAdminUnavailable
	}
	reply := &strategyv1.RiskDiagnosticEvent{
		Id:                   value.ID,
		RecordedAtMs:         value.RecordedAt.UnixMilli(),
		Kind:                 string(value.Kind),
		SnapshotId:           value.SnapshotID,
		SnapshotRevision:     value.SnapshotRevision,
		StrategyId:           value.StrategyID,
		AccountId:            value.AccountID,
		InstrumentId:         value.InstrumentID,
		ConfigVersion:        value.ConfigVersion,
		MarketDataSource:     string(value.Source),
		PublisherEpoch:       value.PublisherEpoch,
		MarketSequence:       value.MarketSequence,
		PublicationRevision:  value.PublicationRevision,
		SourceVersion:        value.SourceVersion,
		MarketMaximumAgeMs:   value.MarketMaximumAge.Milliseconds(),
		ObservedAtMs:         value.ObservedAt.UnixMilli(),
		ExpiresAtMs:          value.ExpiresAt.UnixMilli(),
		RecordedReconciled:   value.RecordedReconciled,
		RecordedAllowNewRisk: value.RecordedAllowNewRisk,
		ReasonCode:           string(value.Reason),
		SnapshotDeleted:      value.SnapshotDeleted,
	}
	if value.FeedID != nil {
		id := *value.FeedID
		reply.MarketFeedId = &id
	}
	if !ValidRiskEvent(reply) {
		return nil, domain.ErrAdminUnavailable
	}
	return reply, nil
}

func riskEventMillis(value time.Time) bool {
	return value.UnixMilli() > 0 && value.Equal(time.UnixMilli(value.UnixMilli()))
}

// ValidRiskEvent accepts historical raw flags, even expired or unreconciled READY.
func ValidRiskEvent(value *strategyv1.RiskDiagnosticEvent) bool {
	return validRiskEventIdentity(value) && validRiskEventObservation(value)
}

func validRiskEventIdentity(value *strategyv1.RiskDiagnosticEvent) bool {
	return value != nil && len(value.ProtoReflect().GetUnknown()) == 0 && value.Id > 0 && value.RecordedAtMs > 0 &&
		value.SnapshotId > 0 && value.SnapshotRevision > 0 && value.StrategyId > 0 && riskEventAccount(value.AccountId) &&
		value.InstrumentId > 0 && value.ConfigVersion > 0 && domain.ValidRiskSnapshotEventKind(domain.RiskSnapshotEventKind(value.Kind)) &&
		(value.Kind != string(domain.RiskSnapshotEventRemoved) || value.SnapshotDeleted)
}

func riskEventAccount(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for index := range len(value) {
		c := value[index]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == ':' {
			continue
		}
		return false
	}
	return true
}

func validRiskEventObservation(value *strategyv1.RiskDiagnosticEvent) bool {
	return domain.MarketSource(value.MarketDataSource).ValidateSelection() == nil &&
		(value.MarketFeedId == nil || *value.MarketFeedId > 0) && value.PublisherEpoch >= 0 && value.MarketSequence >= 0 &&
		value.PublicationRevision >= 0 && value.SourceVersion >= 0 && value.MarketMaximumAgeMs > 0 &&
		value.MarketMaximumAgeMs <= domain.MaxRiskSnapshotAge.Milliseconds() && value.ObservedAtMs > 0 && value.ExpiresAtMs > value.ObservedAtMs &&
		value.ExpiresAtMs-value.ObservedAtMs <= domain.MaxRiskSnapshotAge.Milliseconds() && domain.ValidRiskReason(domain.RiskReason(value.ReasonCode))
}

// ValidRiskEventPage binds ascending event identities to every requested filter.
func ValidRiskEventPage(value *strategyv1.ListRiskEventsReply, filter domain.AdminRiskEventFilter) bool {
	if value == nil || len(value.ProtoReflect().GetUnknown()) != 0 || filter.Validate() != nil || len(value.Events) > filter.Page.Limit || value.NextAfterId < 0 {
		return false
	}
	previous := filter.Page.AfterID
	for _, event := range value.Events {
		if !ValidRiskEvent(event) || event.Id <= previous || !riskEventMatches(event, filter) {
			return false
		}
		previous = event.Id
	}
	return value.NextAfterId == 0 || len(value.Events) == filter.Page.Limit && value.NextAfterId == previous
}

func riskEventMatches(value *strategyv1.RiskDiagnosticEvent, filter domain.AdminRiskEventFilter) bool {
	return (filter.StrategyID == 0 || value.StrategyId == filter.StrategyID) &&
		(filter.AccountID == "" || value.AccountId == filter.AccountID) && (filter.InstrumentID == 0 || value.InstrumentId == filter.InstrumentID) &&
		(filter.Source == domain.MarketSourceUnspecified || value.MarketDataSource == string(filter.Source)) &&
		(filter.Kind == domain.RiskSnapshotEventUnspecified || value.Kind == string(filter.Kind)) &&
		(filter.Reason == domain.RiskReasonUnspecified || value.ReasonCode == string(filter.Reason))
}
