package console

import (
	"context"
	"strings"

	commonmodel "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	model "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
)

// ListTemplates lists registered database templates without implicit parameter defaults.
func (s *Server) ListTemplates(ctx context.Context, in *pb.ListRequest) (*pb.TemplatePage, error) {
	if err := s.authorize(ctx, in.GetActor(), false); err != nil {
		return nil, err
	}
	offset, limit, err := pagination(in.GetPage(), in.GetPageSize())
	if err != nil {
		return nil, err
	}
	query := fresh(ctx, s.db).Model(&model.StrategyTemplate{}).Where("delete_at IS NULL")
	if search := strings.TrimSpace(in.GetSearch()); search != "" {
		query = query.Where("name ILIKE ? OR template_key ILIKE ?", "%"+search+"%", "%"+search+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	var rows []*model.StrategyTemplate
	if err := query.Order("id").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	result := &pb.TemplatePage{
		Items: make([]*pb.Template, 0, len(rows)),
		Total: uint64(total),
	}
	for _, row := range rows {
		result.Items = append(result.Items, &pb.Template{
			Id:                row.Id,
			Key:               row.TemplateKey,
			Name:              row.Name,
			StrategyType:      row.StrategyType,
			Implementation:    row.Implementation,
			Revision:          row.Revision,
			Enabled:           row.Enabled,
			RequiresOrderBook: row.RequiredInputs.GetOrderBook(),
			RequiresAccount:   row.RequiredInputs.GetAccount(),
			TradesMode:        row.RequiredInputs.GetTradesWhenWashMode().String(),
			ExecutionFields:   append([]string{}, row.ParameterContract.GetExecutionFields()...),
			ParameterFields:   append([]string{}, row.ParameterContract.GetAlgorithmFields()...),
		})
	}
	return result, nil
}

// ListInstruments lists instruments visible through the verified account scope.
func (s *Server) ListInstruments(ctx context.Context, in *pb.ListRequest) (*pb.InstrumentPage, error) {
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
	query := fresh(ctx, s.db).Model(&commonmodel.Instrument{}).Where("delete_at IS NULL")
	if in.Actor.TenantId > 0 {
		symbols := scoped(fresh(ctx, s.db).Model(&commonmodel.TradingAccountPolicy{}).Where("delete_at IS NULL"), in.Actor).Select("symbol")
		query = query.Where("symbol IN (?)", symbols)
	}

	if search := strings.TrimSpace(in.GetSearch()); search != "" {
		query = query.Where("symbol ILIKE ?", "%"+search+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	var rows []*commonmodel.Instrument
	if err := query.Order("id").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	result := &pb.InstrumentPage{
		Items: make([]*pb.Instrument, 0, len(rows)),
		Total: uint64(total),
	}
	for _, row := range rows {
		result.Items = append(result.Items, &pb.Instrument{
			Id:              row.Id,
			Symbol:          row.Symbol,
			BaseAsset:       row.BaseAsset,
			QuoteAsset:      row.QuoteAsset,
			Status:          row.Status,
			ExecutionVenue:  row.ExecutionVenue,
			PriceTick:       row.PriceTick,
			QuantityStep:    row.QuantityStep,
			MinimumNotional: row.MinNotional,
		})
	}
	return result, nil
}
