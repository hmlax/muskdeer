package domain

import (
	"context"
	"time"
)

// AdminMarketFilter separates live data from replay and never substitutes a
// different source or product. Zero instrument/source values mean no filter.
type AdminMarketFilter struct {
	Page         AdminPage
	Source       MarketSource
	InstrumentID int64
	Mode         MarketMode
}

// Validate requires an explicit mode and bounded ascending feed cursor.
func (f AdminMarketFilter) Validate() error {
	if f.Page.AfterID < 0 || f.Page.Limit < 1 || f.Page.Limit > MaxAdminPageSize || f.InstrumentID < 0 ||
		f.Source != MarketSourceUnspecified && f.Source.ValidateSelection() != nil ||
		f.Mode != MarketModeLive && f.Mode != MarketModeReplay {
		return ErrInvalidAdminQuery
	}
	return nil
}

// AdminMarketSnapshot distinguishes persisted diagnostics from a reading
// rechecked at database time. PublisherCurrent grants no trading permission.
type AdminMarketSnapshot struct {
	Revision            int64
	PublisherEpoch      int64
	PublisherCurrent    bool
	PublisherExpiresAt  time.Time
	PublishedAt         time.Time
	PublishedValidUntil time.Time
	PublishedQuality    MarketQuality
	PublishedReason     MarketReason
	Reading             MarketReading
}

// AdminMarket retains provisioned identity even if no snapshot exists. Nil
// Snapshot is absence of an observation, never evidence of healthy data.
type AdminMarket struct {
	Feed        MarketFeedDefinition
	Instrument  AdminInstrument
	EvaluatedAt time.Time
	Snapshot    *AdminMarketSnapshot
}

// AdminMarketPage uses feed identities, independently of publication revisions.
type AdminMarketPage struct {
	Items       []AdminMarket
	NextAfterID int64
}

// AdminMarkets reads current viewer-authorized repeatable snapshots without
// acquiring leases, connecting providers, publishing data or changing risk.
type AdminMarkets interface {
	List(context.Context, string, AdminMarketFilter) (AdminMarketPage, error)
	Detail(context.Context, string, int64) (AdminMarket, error)
}
