package domain

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// Market publication errors keep stale revisions distinct from missing data.
const (
	MessageMarketRevision = "market publication revision is no longer current"
	MessageMarketMissing  = "market snapshot has not been published"
)

// Publication errors never authorize use of an older snapshot.
var (
	ErrMarketRevision = errors.New(MessageMarketRevision)
	ErrMarketMissing  = errors.New(MessageMarketMissing)
)

// MarketBaseline is the last accepted source-local reference. It can be stale
// and is never executable market data; restoring it cannot grant HEALTHY.
type MarketBaseline struct {
	Source        MarketSource
	InstrumentID  int64
	Symbol        string
	Mode          MarketMode
	SourceVersion int64
	Mid           decimal.Decimal
	AcceptedAt    time.Time
}

// MarketPublication carries persisted publication fencing and a diagnostic
// baseline separately from the consumer's fresh, selected reading.
type MarketPublication struct {
	FeedID         int64
	OwnerID        string
	OwnerEpoch     int64
	AuthorityUntil time.Time
	Revision       int64
	Reading        MarketReading
	Baseline       MarketBaseline
}

// MarketRepository commits one fenced current snapshot per feed. Publish CAS
// uses a durable revision; Get rechecks publisher authority and database time.
type MarketRepository interface {
	Publish(ctx context.Context, authority LeaseToken, revision int64, reading MarketReading) (MarketPublication, error)
	Get(ctx context.Context, feedID int64) (MarketPublication, error)
}
