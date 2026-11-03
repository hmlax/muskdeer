package himalaya

import (
	"context"
	"errors"
	"time"

	pb "gitlab.com/himalaya/lukla-go/pb"
)

const messageInvalidSDKSnapshot = "himalaya SDK query returned an invalid account scope"

// ErrInvalidSnapshot prevents data from another fund entering a risk decision.
var ErrInvalidSnapshot = errors.New(messageInvalidSDKSnapshot)

// Scope selects an existing exchange account and instrument; queries do not
// create funds or alter account mode, leverage, orders or positions.
type Scope struct {
	UserID int64
	FundID int64
	Symbol string
}

// SDKSnapshot retains the SDK's typed responses. Its separate upstream queries
// are observations, not an atomic exchange snapshot or reconciliation proof.
type SDKSnapshot struct {
	Account   *pb.GetAccountResponse
	Market    *pb.GetMarketResponse
	Positions []*pb.PositionRow
	Orders    []*pb.OrderRow
	StartedAt time.Time
	ReadAt    time.Time
}

// Ping checks the existing SDK endpoint with the configured bounded deadline.
func (c *Client) Ping(ctx context.Context) error {
	if c == nil || c.sdk == nil || ctx == nil {
		return ErrInvalidConfig
	}
	op, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.sdk.Ping(op)
	return err
}

// Read queries the supplied SDK only and verifies every returned fund identity.
func (c *Client) Read(ctx context.Context, scope Scope) (*SDKSnapshot, error) {
	if c == nil || c.sdk == nil || ctx == nil || scope.UserID <= 0 || scope.FundID <= 0 || !validSymbol(scope.Symbol) {
		return nil, ErrInvalidConfig
	}
	op, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	started := time.Now()
	market, err := c.sdk.Raw().GetMarket(op, &pb.GetMarketRequest{Symbol: scope.Symbol})
	if err != nil {
		return nil, err
	}
	account, err := c.sdk.GetAccountForFund(op, scope.UserID, scope.FundID)
	if err != nil {
		return nil, err
	}
	// Read opening obligations before positions: a fill between these queries
	// remains conservatively covered by its preceding order observation.
	orders, err := c.sdk.ListOrdersForFund(op, scope.UserID, scope.FundID, "")
	if err != nil {
		return nil, err
	}
	positions, err := c.sdk.ListPositionsForFund(op, scope.UserID, scope.FundID)
	if err != nil {
		return nil, err
	}
	if market.GetMarket().GetSymbol() != scope.Symbol || account.GetUserId() != scope.UserID || account.GetFundId() != scope.FundID {
		return nil, ErrInvalidSnapshot
	}
	for _, position := range positions.GetPositions() {
		if position.GetUserId() != scope.UserID || position.GetFundId() != scope.FundID {
			return nil, ErrInvalidSnapshot
		}
	}
	for _, order := range orders.GetOrders() {
		if order.GetUserId() != scope.UserID || order.GetFundId() != scope.FundID || order.GetSymbol() != scope.Symbol {
			return nil, ErrInvalidSnapshot
		}
	}
	return &SDKSnapshot{
		Account:   account,
		Market:    market,
		Positions: positions.GetPositions(),
		Orders:    orders.GetOrders(),
		StartedAt: started,
		ReadAt:    time.Now(),
	}, nil
}
