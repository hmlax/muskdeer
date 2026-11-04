// Package gateway defines the execution business boundary for a trading venue.
package gateway

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

// MaximumOperationTimeout bounds both transport attempts and final business admission.
const (
	MaximumOperationTimeout = 5 * time.Second
	messageInvalidConfig    = "venue gateway configuration is invalid"
	messageInvalidSubmit    = "venue gateway submission is invalid"
	messageInvalidSnapshot  = "venue gateway query returned an invalid account scope"
	messageNotDispatched    = "venue gateway callback rejected before dispatch"
)

// ErrInvalidConfig, ErrInvalidSubmit, ErrInvalidSnapshot and ErrNotDispatched
// are stable boundary errors shared by business and transport implementations.
var (
	ErrInvalidConfig   = errors.New(messageInvalidConfig)
	ErrInvalidSubmit   = errors.New(messageInvalidSubmit)
	ErrInvalidSnapshot = errors.New(messageInvalidSnapshot)
	ErrNotDispatched   = errors.New(messageNotDispatched)
)

// Config is immutable connection setup supplied by the execution lifecycle.
type Config struct {
	Target      string
	Token       string
	Timeout     time.Duration
	DialOptions []grpc.DialOption
}

// Scope selects an existing venue fund and instrument.
type Scope struct {
	UserID int64
	FundID int64
	Symbol string
}

// Admission authorizes one durable intent at the final external call.
type Admission struct {
	IntentID int64
	OwnerID  string
	Epoch    uint64
	UserID   int64
	FundID   int64
	ClientID string
	Digest   [32]byte
}

// SubmitRequest carries exact decimals and a durable business identity.
type SubmitRequest struct {
	IntentID int64
	OwnerID  string
	Epoch    uint64
	UserID   int64
	FundID   int64
	Symbol   string
	ClientID string
	Side     domain.Side
	Price    decimal.Decimal
	Quantity decimal.Decimal
	Leverage uint32
}

// SubmitResult is an acknowledgement, not a terminal order observation.
type SubmitResult struct {
	OrderID  uint64
	ClientID string
}

// GetOrderID returns zero when no acknowledgement was received.
func (r *SubmitResult) GetOrderID() uint64 {
	if r == nil {
		return 0
	}
	return r.OrderID
}

// GetClientID returns the acknowledged caller identity or an empty value.
func (r *SubmitResult) GetClientID() string {
	if r == nil {
		return ""
	}
	return r.ClientID
}

// CancelRequest addresses a known immutable venue order ID.
type CancelRequest struct {
	Scope   Scope
	OrderID uint64
}

// CancelAdmission binds one persisted cancellation to its execution owner.
type CancelAdmission struct {
	ID      int64
	OwnerID string
	Epoch   uint64
	Request CancelRequest
	Digest  [32]byte
}

// SubmissionGuard owns the durable transaction and final fencing decision.
type SubmissionGuard interface {
	WithSubmission(context.Context, Admission, func(context.Context) error) error
}

// CancellationGuard owns controlled cancellation admission independently of feeds.
type CancellationGuard interface {
	WithCancellation(context.Context, CancelAdmission, func(context.Context) error) error
}

// Observation contains normalized business values without upstream SDK messages.
// Separate venue queries remain observations rather than an atomic ledger snapshot.
type Observation struct {
	Snapshot      *commonv1.VenueSnapshot
	BasePrecision int32
}

// SyntheticRequest carries a finite, already admitted development tape event.
type SyntheticRequest struct {
	Symbol   string
	Side     domain.Side
	Price    string
	Quantity string
}

// SyntheticResult contains the venue's development tape acknowledgement.
type SyntheticResult struct {
	TradeID     string
	Price       string
	Quantity    string
	TimestampMS uint64
}

// GetTradeID returns the venue event identity or an empty value.
func (r *SyntheticResult) GetTradeID() string {
	if r == nil {
		return ""
	}
	return r.TradeID
}

// GetTimestampMS returns the acknowledged event time or zero.
func (r *SyntheticResult) GetTimestampMS() uint64 {
	if r == nil {
		return 0
	}
	return r.TimestampMS
}

// Client exposes only the transport operations consumed by venue execution.
type Client interface {
	Ping(context.Context) error
	Close(context.Context) error
	Read(context.Context, Scope) (*Observation, error)
	ReadOrders(context.Context, Scope) ([]*commonv1.VenueOrder, error)
	Submit(context.Context, SubmitRequest) (*SubmitResult, error)
	Cancel(context.Context, CancelAdmission) error
	FindKnown(context.Context, Scope, uint64, int32) (*commonv1.VenueOrder, error)
	FindClientOrders(context.Context, Scope, []string, int32) ([]*commonv1.VenueOrder, error)
	PublishSynthetic(context.Context, *SyntheticRequest) (*SyntheticResult, error)
	CheckSyntheticMarket(context.Context, *commonv1.VenueProfilePolicy, *commonv1.SyntheticTapePlan) error
}

// Codec supplies stable wire identities without network or database effects.
// Keeping the codec here preserves existing durable request digests across upgrades.
type Codec interface {
	RequestDigest(SubmitRequest) ([32]byte, error)
	CancelDigest(CancelRequest) ([32]byte, error)
}

// Factory supplies the codec and one lifecycle-owned transport constructor.
type Factory interface {
	Codec
	Dial(context.Context, Config, SubmissionGuard) (Client, error)
}
