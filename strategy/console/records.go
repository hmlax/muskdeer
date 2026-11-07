package console

import (
	"context"
	"strings"

	"gorm.io/gorm"

	execution "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
)

func (s *Server) syntheticScope(ctx context.Context, actor *pb.Actor) *gorm.DB {
	query := fresh(ctx, s.db).Model(&execution.SyntheticTapeIntent{})
	if actor.GetTenantId() > 0 {
		// Legacy slots without an instance identity cannot prove merchant ownership.
		instances := scoped(fresh(ctx, s.db).Model(&strategy.StrategyInstance{}), actor).Select("id")
		query = query.Where("(plan->>'strategy_instance_id')::bigint IN (?)", instances)
	}
	return query
}

func recordFilters(query *gorm.DB, in *pb.RecordListRequest) *gorm.DB {
	if in.GetSymbol() != "" {
		query = query.Where("symbol = ?", strings.ToUpper(strings.TrimSpace(in.Symbol)))
	}
	return query
}

// ListOrderRecords lists immutable submission history within the verified account scope.
func (s *Server) ListOrderRecords(ctx context.Context, in *pb.RecordListRequest) (*pb.OrderPage, error) {
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
	query := recordFilters(scoped(fresh(ctx, s.db).Model(&execution.VenueOrderIntent{}).Where("delete_at IS NULL"), in.Actor), in)
	if in.AccountId != "" {
		query = query.Where("account_id = ?", in.AccountId)
	}
	if in.State != "" {
		query = query.Where("state = ?", in.State)
	}
	if in.InstanceId > 0 {
		query = query.Where("strategy_instance_id = ?", in.InstanceId)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	var rows []*execution.VenueOrderIntent
	if err := query.Order("id DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	result := &pb.OrderPage{
		Items: make([]*pb.OrderRecord, 0, len(rows)),
		Total: uint64(total),
	}
	for _, row := range rows {
		result.Items = append(result.Items, &pb.OrderRecord{
			Id:               row.Id,
			CreatedAtMs:      row.CreateAt,
			UpdatedAtMs:      row.UpdateAt,
			AccountId:        row.AccountId,
			Symbol:           row.Symbol,
			ClientId:         row.ClientId,
			Side:             row.Side,
			Price:            row.Price,
			Quantity:         row.Quantity,
			Leverage:         row.Leverage,
			State:            row.State,
			OrderId:          row.OrderId,
			ReservedNotional: row.ReservedNotional,
			InstanceId:       row.GetStrategyInstanceId(),
			Revision:         row.GetStrategyRevision(),
		})
	}
	return result, nil
}

// ListCancellations lists cancellation receipts within the verified account scope.
func (s *Server) ListCancellations(ctx context.Context, in *pb.RecordListRequest) (*pb.CancellationPage, error) {
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
	query := recordFilters(scoped(fresh(ctx, s.db).Model(&execution.VenueCancellation{}).Where("delete_at IS NULL"), in.Actor), in)
	if in.AccountId != "" {
		query = query.Where("account_id = ?", in.AccountId)
	}
	if in.State != "" {
		query = query.Where("state = ?", in.State)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	var rows []*execution.VenueCancellation
	if err := query.Order("id DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	result := &pb.CancellationPage{
		Items: make([]*pb.Cancellation, 0, len(rows)),
		Total: uint64(total),
	}
	for _, row := range rows {
		result.Items = append(result.Items, &pb.Cancellation{
			Id:          row.Id,
			CreatedAtMs: row.CreateAt,
			UpdatedAtMs: row.UpdateAt,
			AccountId:   row.AccountId,
			Symbol:      row.Symbol,
			OrderId:     row.OrderId,
			State:       row.State,
		})
	}
	return result, nil
}

// ListSyntheticRecords lists development publications with provable instance ownership.
func (s *Server) ListSyntheticRecords(ctx context.Context, in *pb.RecordListRequest) (*pb.SyntheticPage, error) {
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
	query := recordFilters(s.syntheticScope(ctx, in.Actor).Where("delete_at IS NULL"), in)
	if in.State != "" {
		query = query.Where("result->>'state' = ?", in.State)
	}
	if in.RunId != "" {
		query = query.Where("run_id = ?", in.RunId)
	}
	if in.InstanceId > 0 {
		query = query.Where("(plan->>'strategy_instance_id')::bigint = ?", in.InstanceId)
	}
	if in.AccountId != "" {
		instances := fresh(ctx, s.db).Model(&strategy.StrategyInstance{}).Where("account_id = ?", in.AccountId).Select("id")
		query = query.Where("(plan->>'strategy_instance_id')::bigint IN (?)", instances)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	var rows []*execution.SyntheticTapeIntent
	if err := query.Order("id DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	result := &pb.SyntheticPage{
		Items: make([]*pb.SyntheticRecord, 0, len(rows)),
		Total: uint64(total),
	}
	for _, row := range rows {
		result.Items = append(result.Items, &pb.SyntheticRecord{
			Id:            row.Id,
			CreatedAtMs:   row.CreateAt,
			RunId:         row.RunId,
			Symbol:        row.Symbol,
			Sequence:      uint64(row.Sequence),
			Side:          row.Plan.GetSide(),
			Price:         row.Plan.GetPrice(),
			Quantity:      row.Plan.GetQuantity(),
			Notional:      row.Notional,
			State:         row.Result.GetState(),
			TradeId:       row.Result.GetTradeId(),
			InstanceId:    row.Plan.GetStrategyInstanceId(),
			SourceTradeId: row.Plan.GetSourceTrade().GetEventId(),
		})
	}
	return result, nil
}
