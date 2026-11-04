package himalaya

import (
	"context"

	pb "gitlab.com/himalaya/lukla-go/pb"
	"google.golang.org/protobuf/proto"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

const (
	historyLookupPageSize = 200
	historyLookupPages    = 10
)

func (s *Store) pendingClientIDs(ctx context.Context, account string) ([]string, error) {
	var rows []*executionmodelv1.VenueOrderIntent
	if err := s.db.WithContext(ctx).Model(&executionmodelv1.VenueOrderIntent{}).
		Where("account_id = ? AND state = ? AND order_id = '' AND historical_approval_reference = '' AND delete_at IS NULL", account, IntentDispatched).
		Select("ClientId").Limit(257).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) > 256 {
		return nil, ErrRiskLimit
	}
	clients := make([]string, 0, len(rows))
	for _, row := range rows {
		clients = append(clients, row.ClientId)
	}
	return clients, nil
}

// findClientOrders returns positive native facts only. Missing history is never
// a rejection or cancellation proof. Correlation does not imply SDK deduplication;
// conflicting native identities for a requested client remain invalid.
func (c *Client) findClientOrders(ctx context.Context, scope Scope, clients []string, basePrecision int32) ([]*commonv1.HimalayaOrder, error) {
	if c == nil || c.sdk == nil || ctx == nil || !validClientRecoveryScope(scope, basePrecision) || len(clients) == 0 || len(clients) > 256 {
		return nil, ErrInvalidSubmit
	}
	wanted := make(map[string]bool, len(clients))
	for _, client := range clients {
		if !validIdentity(client) || wanted[client] {
			return nil, ErrInvalidSubmit
		}
		wanted[client] = true
	}
	found := make(map[string]*commonv1.HimalayaOrder)
	op, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var cursor *pb.OrderHistoryCursor
	for range historyLookupPages {
		response, err := c.sdk.Raw().ListOrders(op, &pb.ListOrdersRequest{
			UserId:  scope.UserID,
			FundId:  scope.FundID,
			Symbol:  scope.Symbol,
			History: true,
			Limit:   historyLookupPageSize,
			Before:  cursor,
		})
		if err != nil {
			return nil, err
		}
		for _, row := range response.GetOrders() {
			if row.GetUserId() != scope.UserID || row.GetFundId() != scope.FundID || row.GetSymbol() != scope.Symbol {
				return nil, ErrInvalidSnapshot
			}
			if !wanted[row.ClientId] {
				continue
			}
			order, err := normalizeOrder(row, scope, -basePrecision)
			if err != nil || !native.Terminal(order.GetStatus()) {
				return nil, ErrInvalidSnapshot
			}
			if previous := found[row.ClientId]; previous != nil && !proto.Equal(previous, order) {
				return nil, ErrInvalidSnapshot
			}
			found[row.ClientId] = order
		}
		if response.GetNext() == nil {
			break
		}
		if response.Next.OrderId == 0 || cursor != nil && response.Next.OrderId >= cursor.OrderId {
			return nil, ErrInvalidSnapshot
		}
		cursor = response.Next
	}
	orders := make([]*commonv1.HimalayaOrder, 0, len(found))
	for _, client := range clients {
		if order := found[client]; order != nil {
			orders = append(orders, order)
		}
	}
	return orders, nil
}

func validClientRecoveryScope(scope Scope, precision int32) bool {
	return scope.UserID > 0 && scope.FundID > 0 && validSymbol(scope.Symbol) && precision >= 0 && precision <= 18
}
