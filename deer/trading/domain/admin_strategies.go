package domain

import (
	"context"
	"errors"
	"time"
)

// Management query limits and fixed public failures.
const (
	MaxAdminPageSize         = 100
	MessageInvalidAdminQuery = "invalid management query"
	MessageAdminNotFound     = "management resource not found"
	MessageAdminUnavailable  = "management query temporarily unavailable"
)

// Query failures contain no persistence details or authentication secrets.
var (
	ErrInvalidAdminQuery = errors.New(MessageInvalidAdminQuery)
	ErrAdminNotFound     = errors.New(MessageAdminNotFound)
	ErrAdminUnavailable  = errors.New(MessageAdminUnavailable)
)

// AdminPage uses an exclusive ascending identity cursor, without an offset scan.
type AdminPage struct {
	AfterID int64
	Limit   int
}

// AdminStrategy is a snapshot observation, never an execution permission.
type AdminStrategy struct {
	Spec      StrategySpec
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AdminStrategyPage bounds each response independently of the total row count.
type AdminStrategyPage struct {
	Items       []AdminStrategy
	NextAfterID int64
}

// AdminConfigVersion exposes explicit typed parameters. Legacy schema zero has
// no runnable parameters; readers must not invent defaults for it.
type AdminConfigVersion struct {
	ID            int64
	StrategyID    int64
	Version       int64
	Source        MarketSource
	CodeVersion   string
	CreatedBy     string
	SchemaVersion int64
	Parameters    StrategyParameters
	CreatedAt     time.Time
}

// AdminConfigPage is scoped to exactly one strategy instance.
type AdminConfigPage struct {
	Items       []AdminConfigVersion
	NextAfterID int64
}

// AdminStrategyDetail reads requested and active versions in the same snapshot.
type AdminStrategyDetail struct {
	Strategy  AdminStrategy
	Requested AdminConfigVersion
	Active    *AdminConfigVersion
}

// AdminStrategies checks viewer authority inside each read-only transaction.
// A prior HTTP identity check cannot replace this business-service boundary.
type AdminStrategies interface {
	List(context.Context, string, AdminPage) (AdminStrategyPage, error)
	Detail(context.Context, string, int64) (AdminStrategyDetail, error)
	Versions(context.Context, string, int64, AdminPage) (AdminConfigPage, error)
}
