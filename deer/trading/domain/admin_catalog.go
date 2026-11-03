package domain

import (
	"context"

	"github.com/shopspring/decimal"
)

// AdminAccount describes a provisioned L1 account; listing it does not grant
// funds, execution ownership, or permission to trade.
type AdminAccount struct {
	ID           int64
	AccountID    string
	SubjectID    string
	Enabled      bool
	QuoteFeeRate decimal.Decimal
	Revision     int64
}

// AdminAccountPage uses the persisted identity, not the business account key.
type AdminAccountPage struct {
	Items       []AdminAccount
	NextAfterID int64
}

// AdminInstrument separates execution metadata from external feed identity.
type AdminInstrument struct {
	Rules          StrategyInstrument
	ExecutionVenue string
	Symbol         string
	BaseAsset      string
	QuoteAsset     string
}

// AdminInstrumentPage includes halted metadata explicitly, without selecting it
// automatically or hiding it behind another instrument's healthy feed.
type AdminInstrumentPage struct {
	Items       []AdminInstrument
	NextAfterID int64
}

// AdminFeedPage lists bindings only. A persisted binding is not evidence of a
// current publisher, a fresh snapshot, or permission to activate risk.
type AdminFeedPage struct {
	Items       []MarketFeedDefinition
	NextAfterID int64
}

// AdminCatalog reads provisioned choices under current viewer authority in
// each bounded snapshot. It does not create account, instrument, or feed rows.
type AdminCatalog interface {
	Accounts(context.Context, string, AdminPage) (AdminAccountPage, error)
	Instruments(context.Context, string, AdminPage) (AdminInstrumentPage, error)
	Feeds(context.Context, string, int64, MarketMode, AdminPage) (AdminFeedPage, error)
}
