package console

import (
	"context"
	"strings"
	"time"

	common "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

// ListAccounts returns scoped business account policy without granting financial edits.
func (s *Server) ListAccounts(ctx context.Context, in *pb.ListRequest) (*pb.AccountPage, error) {
	if err := s.authorize(ctx, in.GetActor(), false); err != nil {
		return nil, err
	}
	if err := accountScope(in.GetActor()); err != nil {
		return nil, err
	}
	offset, limit, err := pagination(in.GetPage(), in.GetPageSize())
	if err != nil {
		return nil, err
	}
	query := scoped(fresh(ctx, s.db).Model(&common.TradingAccountPolicy{}).Where("delete_at IS NULL"), in.Actor)
	if search := strings.TrimSpace(in.GetSearch()); search != "" {
		query = query.Where("symbol ILIKE ?", "%"+search+"%")
	}
	if in.GetAccountId() != "" {
		query = query.Where("account_id = ?", in.AccountId)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	var rows []*common.TradingAccountPolicy
	if err := query.Order("id").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	result := &pb.AccountPage{
		Items: make([]*pb.Account, 0, len(rows)),
		Total: uint64(total),
	}
	for _, row := range rows {
		result.Items = append(result.Items, accountDTO(row))
	}
	return result, nil
}

func accountDTO(row *common.TradingAccountPolicy) *pb.Account {
	return &pb.Account{
		Id:                   row.Id,
		AccountId:            row.AccountId,
		UserId:               row.UserId,
		FundId:               row.FundId,
		Symbol:               row.Symbol,
		MaximumNotional:      row.MaximumNotional,
		MaximumOrderNotional: row.MaximumOrderNotional,
		Leverage:             row.Leverage,
		Enabled:              row.Enabled,
		ObservedNotional:     row.ObservedNotional,
		ReservedNotional:     row.ReservedNotional,
		AvailableQuote:       row.AvailableQuote,
		SnapshotExpiresAtMs:  row.SnapshotExpiresAt,
	}
}

// GetOverview counts records within the verified account scope.
func (s *Server) GetOverview(ctx context.Context, in *pb.ActorRequest) (*pb.Overview, error) {
	if err := s.authorize(ctx, in.GetActor(), false); err != nil {
		return nil, err
	}
	if err := accountScope(in.GetActor()); err != nil {
		return nil, err
	}
	var templates, instances, running, instruments, accounts, orders, published, unknown int64
	if err := fresh(ctx, s.db).Model(&strategy.StrategyTemplate{}).Where("delete_at IS NULL").Count(&templates).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	q := scoped(fresh(ctx, s.db).Model(&strategy.StrategyInstance{}).Where("delete_at IS NULL"), in.Actor)
	if err := q.Count(&instances).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	if err := q.Where("desired_running = ?", true).Count(&running).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	budget := scoped(fresh(ctx, s.db).Model(&common.TradingAccountPolicy{}).Where("delete_at IS NULL"), in.Actor)
	if err := budget.Count(&accounts).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	products := fresh(ctx, s.db).Model(&common.Instrument{}).Where("delete_at IS NULL")
	if in.Actor.TenantId > 0 {
		symbols := scoped(fresh(ctx, s.db).Model(&common.TradingAccountPolicy{}).Where("delete_at IS NULL"), in.Actor).Select("symbol")
		products = products.Where("symbol IN (?)", symbols)
	}
	if err := products.Count(&instruments).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	if err := scoped(fresh(ctx, s.db).Model(&execution.VenueOrderIntent{}).Where("delete_at IS NULL"), in.Actor).Count(&orders).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	for _, entry := range []struct {
		state string
		count *int64
	}{
		{
			state: native.SyntheticPublished,
			count: &published,
		},
		{
			state: native.SyntheticUnknown,
			count: &unknown,
		},
	} {
		query := s.syntheticScope(ctx, in.Actor).Where("delete_at IS NULL AND result->>'state' = ?", entry.state)
		if err := query.Count(entry.count).Error; err != nil {
			return nil, s.fail(ctx, err)
		}
	}
	return &pb.Overview{
		Templates:          uint64(templates),
		Instances:          uint64(instances),
		DesiredRunning:     uint64(running),
		Instruments:        uint64(instruments),
		Accounts:           uint64(accounts),
		OrderRecords:       uint64(orders),
		SyntheticPublished: uint64(published),
		SyntheticUnknown:   uint64(unknown),
		ObservedAtMs:       time.Now().UnixMilli(),
	}, nil
}
