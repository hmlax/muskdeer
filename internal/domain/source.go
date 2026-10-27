package domain

import (
	"errors"
	"strings"
)

// MarketSource identifies a data provider independently of execution venue.
type MarketSource string

// Source identifiers are centralized; candidates are recognized but not selected.
const (
	MarketSourceUnspecified MarketSource = ""
	MarketSourceBinance     MarketSource = "binance"
	MarketSourceOKX         MarketSource = "okx"
	MarketSourceBitget      MarketSource = "bitget"
	MarketSourceBybit       MarketSource = "bybit"
	MarketSourceAliasBitgit              = "bitgit"
)

// Source validation messages are stable public business messages.
const (
	MessageUnknownMarketSource     = "market data source is unknown"
	MessageMarketSourceUnavailable = "market data source is a candidate and cannot be selected"
)

// Source errors keep unknown identifiers distinct from recognized candidates.
var (
	ErrUnknownMarketSource     = errors.New(MessageUnknownMarketSource)
	ErrMarketSourceUnavailable = errors.New(MessageMarketSourceUnavailable)
)

// ParseMarketSource normalizes case/outer whitespace and the recorded bitgit
// alias. Empty or unknown values never select a default or another provider.
func ParseMarketSource(input string) (MarketSource, error) {
	value := strings.ToLower(strings.TrimSpace(input))
	if value == MarketSourceAliasBitgit {
		value = string(MarketSourceBitget)
	}
	source := MarketSource(value)
	switch source {
	case MarketSourceBinance, MarketSourceOKX, MarketSourceBitget, MarketSourceBybit:
		return source, nil
	default:
		return MarketSourceUnspecified, ErrUnknownMarketSource
	}
}

// ValidateSelection checks first-batch eligibility only. Adapter enablement,
// instrument support and current health are separate mandatory runtime checks.
func (source MarketSource) ValidateSelection() error {
	switch source {
	case MarketSourceBinance, MarketSourceOKX:
		return nil
	case MarketSourceBitget, MarketSourceBybit:
		return ErrMarketSourceUnavailable
	default:
		return ErrUnknownMarketSource
	}
}
