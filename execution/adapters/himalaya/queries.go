package himalaya

import (
	"context"
	"math"
	"time"

	pb "gitlab.com/himalaya/lukla-go/pb"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

// FindKnown seeks the native unique ID in durable terminal history. The SDK's
// cursor is an exclusive order-ID boundary; no client-ID uniqueness is assumed.
// A missing row leaves the outcome unknown and cannot release a reservation.
func (c *Client) FindKnown(ctx context.Context, scope Scope, id uint64, basePrecision int32) (*commonv1.VenueOrder, error) {
	if c == nil || c.sdk == nil || ctx == nil || id == 0 || id == math.MaxUint64 || basePrecision < 0 || basePrecision > 18 ||
		scope.UserID <= 0 || scope.FundID <= 0 || !validSymbol(scope.Symbol) {
		return nil, ErrInvalidSubmit
	}
	op, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	response, err := c.sdk.Raw().ListOrders(op, &pb.ListOrdersRequest{
		UserId:  scope.UserID,
		FundId:  scope.FundID,
		Symbol:  scope.Symbol,
		History: true,
		Limit:   1,
		Before:  &pb.OrderHistoryCursor{OrderId: id + 1},
	})
	if err != nil {
		return nil, err
	}
	if len(response.Orders) != 1 || response.Orders[0].OrderId != id {
		return nil, domain.ErrOutcomeUnknown
	}
	return normalizeOrder(response.Orders[0], scope, -basePrecision)
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
func (c *Client) readSDK(ctx context.Context, scope Scope) (*SDKSnapshot, error) {
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

// Read returns normalized business values without exposing upstream SDK messages.
func (c *Client) Read(ctx context.Context, scope Scope) (*gateway.Observation, error) {
	raw, err := c.readSDK(ctx, scope)
	if err != nil {
		return nil, err
	}
	snapshot, err := Normalize(raw, scope)
	if err != nil {
		return nil, err
	}
	return &gateway.Observation{
		Snapshot:      snapshot,
		BasePrecision: int32(raw.Market.Market.EffectiveBasePrecision),
	}, nil
}

// ReadOrders supports controlled cancellation even when mark-price health is invalid.
func (c *Client) ReadOrders(ctx context.Context, scope Scope) ([]*commonv1.VenueOrder, error) {
	raw, err := c.readSDK(ctx, scope)
	if err != nil {
		return nil, err
	}
	precision := raw.Market.GetMarket().GetEffectiveBasePrecision()
	if precision < 0 || precision > 18 {
		return nil, ErrInvalidSnapshot
	}
	orders := make([]*commonv1.VenueOrder, 0, len(raw.Orders))
	for _, row := range raw.Orders {
		order, err := normalizeOrder(row, scope, -int32(precision))
		if err != nil {
			return nil, err
		}
		orders = append(orders, order)
	}
	return orders, nil
}
