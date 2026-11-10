package himalaya

import (
	"context"
	"math"

	"github.com/shopspring/decimal"
	pb "gitlab.com/himalaya/lukla-go/pb"
	"google.golang.org/protobuf/proto"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

// ReadGrid accepts the venue's one-way or one-long/one-short hedge mode. The
// long-grid decision rejects shorts and targets the returned long position ID.
func (c *Client) ReadGrid(ctx context.Context, scope Scope) (*gateway.Observation, error) {
	raw, err := c.readSDK(ctx, scope)
	if err != nil {
		return nil, err
	}
	snapshot, err := normalizeSDK(raw, scope, true)
	if err != nil {
		return nil, err
	}
	return &gateway.Observation{
		Snapshot:      snapshot,
		BasePrecision: int32(raw.Market.Market.EffectiveBasePrecision),
	}, nil
}

// SubmitGrid makes one IOC limit request or one market stop-loss reduction.
func (c *Client) SubmitGrid(ctx context.Context, request gateway.GridRequest) (*gateway.SubmitResult, error) {
	if c == nil || c.sdk == nil || ctx == nil || !validClientRecoveryScope(request.Scope, 0) || !validIdentity(request.ClientID) || !request.Quantity.IsPositive() || request.Leverage == 0 || request.Leverage > 150 {
		return nil, ErrInvalidSubmit
	}
	if request.ReduceOnly {
		if request.Side != domain.SideSell || request.PositionID == "" {
			return nil, ErrInvalidSubmit
		}
	} else if request.Side != domain.SideBuy || request.Market {
		return nil, ErrInvalidSubmit
	}
	if !request.Market && !request.Price.IsPositive() {
		return nil, ErrInvalidSubmit
	}
	kind := pb.OrderKind_ORDER_KIND_LIMIT
	price := request.Price.String()
	if request.Market {
		kind = pb.OrderKind_ORDER_KIND_MARKET
		price = "0"
	}
	input := &pb.PlaceRequest{
		UserId:       request.Scope.UserID,
		FundId:       request.Scope.FundID,
		Symbol:       request.Scope.Symbol,
		Side:         pb.Side_SIDE_BUY,
		OrderKind:    kind,
		Price:        price,
		Quantity:     request.Quantity.String(),
		TimeInForce:  pb.TimeInForce_TIME_IN_FORCE_IOC,
		ReduceOnly:   request.ReduceOnly,
		Leverage:     int32(request.Leverage),
		PositionType: pb.PositionType_POSITION_TYPE_CROSS,
		PositionId:   request.PositionID,
		ClientId:     request.ClientID,
	}
	if request.ReduceOnly {
		input.Side = pb.Side_SIDE_SELL
	}
	op, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if err := op.Err(); err != nil {
		return nil, err
	}
	response, err := c.sdk.Place(op, input)
	if err != nil {
		return submitResult(response), err
	}
	if response.GetOrderId() == 0 || response.GetClientId() != request.ClientID {
		return submitResult(response), domain.ErrOutcomeUnknown
	}
	return submitResult(response), nil
}

// FindGridOrder never treats missing history as proof of rejection. ACK IDs use
// a single indexed cursor read; only a lost ACK searches bounded account history.
func (c *Client) FindGridOrder(ctx context.Context, scope Scope, clientID string, id uint64, precision int32) (*gateway.GridOrder, error) {
	if c == nil || c.sdk == nil || ctx == nil || !validClientRecoveryScope(scope, precision) || !validIdentity(clientID) || id == math.MaxUint64 {
		return nil, ErrInvalidSubmit
	}
	op, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var cursor *pb.OrderHistoryCursor
	limit := uint32(historyLookupPageSize)
	if id > 0 {
		cursor = &pb.OrderHistoryCursor{OrderId: id + 1}
		limit = 1
	}
	var found *gateway.GridOrder
	for range historyLookupPages {
		response, err := c.sdk.Raw().ListOrders(op, &pb.ListOrdersRequest{
			UserId:  scope.UserID,
			FundId:  scope.FundID,
			Symbol:  scope.Symbol,
			History: true,
			Limit:   limit,
			Before:  cursor,
		})
		if err != nil {
			return nil, err
		}
		for _, row := range response.GetOrders() {
			if row.UserId != scope.UserID || row.FundId != scope.FundID || row.Symbol != scope.Symbol {
				return nil, ErrInvalidSnapshot
			}
			if row.ClientId != clientID || id > 0 && row.OrderId != id {
				continue
			}
			result, err := gridOrder(row, precision)
			if err != nil {
				return nil, err
			}
			if found != nil && found.OrderID != result.OrderID {
				return nil, ErrInvalidSnapshot
			}
			found = result
		}
		if id > 0 || response.GetNext() == nil {
			break
		}
		if response.Next.OrderId == 0 || cursor != nil && response.Next.OrderId >= cursor.OrderId {
			return nil, ErrInvalidSnapshot
		}
		cursor = response.Next
	}
	if found == nil {
		return nil, domain.ErrOutcomeUnknown
	}
	return found, nil
}

func gridOrder(row *pb.OrderRow, precision int32) (*gateway.GridOrder, error) {
	if row.OrderId == 0 || row.ExecutedQuantity > row.Quantity || !native.Terminal(row.Status) {
		return nil, domain.ErrOutcomeUnknown
	}
	notional, err := native.Value(row.ExecutedNotional, false, true)
	if err != nil || row.ExecutedQuantity > 0 && !notional.IsPositive() {
		return nil, ErrInvalidSnapshot
	}
	side := domain.SideBuy
	if row.Side == pb.Side_SIDE_SELL {
		side = domain.SideSell
	} else if row.Side != pb.Side_SIDE_BUY {
		return nil, ErrInvalidSnapshot
	}
	return &gateway.GridOrder{
		OrderID:          row.OrderId,
		ClientID:         row.ClientId,
		Side:             side,
		ReduceOnly:       !row.IsOpenPosition,
		Quantity:         decimal.NewFromUint64(row.Quantity).Shift(-precision).String(),
		ExecutedQuantity: decimal.NewFromUint64(row.ExecutedQuantity).Shift(-precision).String(),
		ExecutedNotional: notional.String(),
		Status:           row.Status,
	}, nil
}

func normalizeGridOrder(row *pb.OrderRow, scope Scope, base int32, mark string) (*common.VenueOrder, error) {
	if row.GetPrice() != "0" {
		return normalizeOrder(row, scope, base)
	}
	snapshot := proto.CloneOf(row)
	snapshot.Price = snapshot.ReservePrice
	if value, err := native.Value(snapshot.Price, false, false); err != nil || !value.IsPositive() {
		snapshot.Price = mark
	}
	return normalizeOrder(snapshot, scope, base)
}
