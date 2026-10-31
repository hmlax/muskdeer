package strategywire

import (
	"math"
	"strings"
	"unicode/utf8"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// ValidOverview binds full-scope counters and a bounded page to their filters.
// It validates sampled lease facts without interpreting them as trading grants.
func ValidOverview(value *strategyv1.GetStrategyOverviewReply, filter domain.AdminOverviewFilter) bool {
	if value == nil || len(value.ProtoReflect().GetUnknown()) != 0 || filter.Validate() != nil || value.ObservedAtMs <= 0 ||
		value.Total < 0 || value.RequestedRunning < 0 || value.RequestedRunning > value.Total || value.NextAfterId < 0 ||
		len(value.StoredStates) > 9 || len(value.Items) > filter.Page.Limit || int64(len(value.Items)) > value.Total {
		return false
	}
	counts, valid := overviewCounts(value)
	if !valid {
		return false
	}
	previous, requested := filter.Page.AfterID, int64(0)
	for _, item := range value.Items {
		if item == nil || len(item.ProtoReflect().GetUnknown()) != 0 || !overviewStrategy(item.Strategy) {
			return false
		}
		s := item.Strategy
		if s.Id <= previous || filter.AccountID != "" && s.AccountId != filter.AccountID ||
			filter.Source != domain.MarketSourceUnspecified && s.RequestedMarketDataSource != string(filter.Source) ||
			counts[s.State] <= 0 || !overviewLease(item.Lease, s, value.ObservedAtMs) {
			return false
		}
		counts[s.State]--
		if domain.StrategyState(s.DesiredState) == domain.StrategyStateRunning {
			requested++
		}
		previous = s.Id
	}
	if requested > value.RequestedRunning || int64(len(value.Items))-requested > value.Total-value.RequestedRunning {
		return false
	}
	return overviewCursor(value, filter.Page, previous)
}

func overviewCursor(value *strategyv1.GetStrategyOverviewReply, page domain.AdminPage, last int64) bool {
	if page.AfterID == 0 && (int64(len(value.Items)) < value.Total && len(value.Items) < page.Limit ||
		(value.NextAfterId != 0) != (value.Total > int64(len(value.Items)))) {
		return false
	}
	return value.NextAfterId == 0 || len(value.Items) == page.Limit && value.NextAfterId == last && value.Total > int64(len(value.Items))
}

func overviewCounts(value *strategyv1.GetStrategyOverviewReply) (map[string]int64, bool) {
	counts := make(map[string]int64, len(value.StoredStates))
	var total int64
	previous := ""
	for _, entry := range value.StoredStates {
		if entry == nil || len(entry.ProtoReflect().GetUnknown()) != 0 || !domain.ValidStrategyState(domain.StrategyState(entry.State)) ||
			entry.State <= previous || entry.Count <= 0 || total > math.MaxInt64-entry.Count {
			return nil, false
		}
		total += entry.Count
		counts[entry.State], previous = entry.Count, entry.State
	}
	return counts, total == value.Total
}

func overviewStrategy(s *strategyv1.Strategy) bool {
	if s == nil || len(s.ProtoReflect().GetUnknown()) != 0 || s.Id <= 0 || s.InstrumentId <= 0 || s.Revision <= 0 ||
		!overviewText(s.InstanceKey, 64) || !riskEventAccount(s.AccountId) || !overviewText(s.CodeVersion, 64) ||
		!domain.ValidStrategyState(domain.StrategyState(s.State)) || !riskDesiredState(domain.StrategyState(s.DesiredState)) ||
		(s.Template != string(domain.StrategyTemplateGrid) && s.Template != string(domain.StrategyTemplateMarketMaker)) ||
		s.RequestedConfigVersion <= 0 || domain.MarketSource(s.RequestedMarketDataSource).ValidateSelection() != nil ||
		(s.MarketMode != string(domain.MarketModeLive) && s.MarketMode != string(domain.MarketModeReplay)) ||
		s.RuntimeOwnerEpoch < 0 || (s.RuntimeOwnerEpoch == 0) != (s.RuntimeOwnerId == "") ||
		(s.RuntimeOwnerId != "" && !overviewText(s.RuntimeOwnerId, 128)) || s.CreatedAtMs <= 0 || s.UpdatedAtMs < s.CreatedAtMs {
		return false
	}
	if (s.ActiveConfigVersion == nil) != (s.ActiveMarketDataSource == nil) {
		return false
	}
	return s.ActiveConfigVersion == nil || *s.ActiveConfigVersion > 0 && *s.ActiveConfigVersion <= s.RequestedConfigVersion &&
		domain.MarketSource(*s.ActiveMarketDataSource).ValidateSelection() == nil
}

func overviewText(value string, maximum int) bool {
	return len(value) > 0 && len(value) <= maximum*4 && utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximum && !strings.ContainsRune(value, 0)
}

func overviewLease(l *strategyv1.StrategyLeaseObservation, s *strategyv1.Strategy, observed int64) bool {
	if l == nil {
		return true
	}
	return len(l.ProtoReflect().GetUnknown()) == 0 && l.Epoch >= 0 && l.ExpiresAtMs >= 0 &&
		(l.Epoch > 0) == l.OwnerRecorded && (l.OwnerRecorded || l.ExpiresAtMs == 0) &&
		l.UnexpiredAtObservation == (l.OwnerRecorded && l.ExpiresAtMs > observed) &&
		(!l.RuntimeMatchesLease || l.OwnerRecorded && s.RuntimeOwnerId != "" && s.RuntimeOwnerEpoch == l.Epoch)
}
