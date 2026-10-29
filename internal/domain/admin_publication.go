package domain

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

// Configuration publication identifiers and fixed boundary failure.
const (
	AdminStrategyPublish           = "strategy.publish"
	MessageInvalidAdminPublication = "invalid strategy configuration publication or provisioned dependency"
)

// ErrInvalidAdminPublication never includes submitted parameters or identities.
var ErrInvalidAdminPublication = errors.New(MessageInvalidAdminPublication)

// AdminPublishStrategy replaces requested input while preserving the instance's
// immutable identity, desired target, actual state and active pair.
type AdminPublishStrategy struct {
	Key                   string
	StrategyID            int64
	ExpectedRevision      int64
	ExpectedConfigVersion int64
	Source                MarketSource
	Parameters            StrategyParameters
	Reason                string
}

// Normalize checks optimistic identity and bounds decimals before arithmetic.
func (r AdminPublishStrategy) Normalize() (AdminPublishStrategy, error) {
	r.Reason = strings.TrimSpace(r.Reason)
	if r.StrategyID <= 0 || r.ExpectedRevision <= 0 || r.ExpectedRevision == math.MaxInt64 ||
		r.ExpectedConfigVersion <= 0 || r.ExpectedConfigVersion == math.MaxInt64 || len(r.Key) < 16 || !intentIdentityPart(r.Key, 64) ||
		r.Source.ValidateSelection() != nil || !validAdminStrategyParameters(r.Parameters) ||
		!utf8.ValidString(r.Reason) || len(r.Reason) == 0 || len(r.Reason) > 256 || strings.ContainsRune(r.Reason, 0) {
		return AdminPublishStrategy{}, ErrInvalidAdminPublication
	}
	return r, nil
}

// AdminPublicationReceipt proves an immutable requested version was committed.
// It does not claim the runtime has activated that version or resumed trading.
type AdminPublicationReceipt struct {
	ID                    int64
	ActorUserID           int64
	StrategyID            int64
	PreviousConfigVersion int64
	ConfigVersion         int64
	PreviousRevision      int64
	Revision              int64
	Source                MarketSource
	Reason                string
	CreatedAt             time.Time
}

// AdminStrategyPublisher checks current operator authority in the transaction.
type AdminStrategyPublisher interface {
	Publish(context.Context, string, string, AdminPublishStrategy) (AdminPublicationReceipt, error)
}
