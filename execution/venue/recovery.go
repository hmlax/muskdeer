package venue

import (
	"context"

	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
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
