package repository

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type adminTradeRelations struct {
	orders     map[int64]*modelv1.SimulatorOrder
	commands   map[int64]*modelv1.SimulatorCommand
	intents    map[int64]*modelv1.OrderIntent
	sources    map[int64]domain.MarketSource
	collected  map[string]*modelv1.PortfolioFill
	quoteAsset string
}

func readAdminTrades(ctx context.Context, tx *gorm.DB, instance *modelv1.StrategyInstance, rows []*modelv1.SimulatorFill) ([]domain.AdminTrade, error) {
	result := make([]domain.AdminTrade, 0, len(rows))
	if len(rows) == 0 {
		return result, nil
	}
	relations, err := readAdminTradeRelations(ctx, tx, instance, rows)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		value, err := adminTradeValue(instance, row, relations)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func readAdminTradeRelations(ctx context.Context, tx *gorm.DB, instance *modelv1.StrategyInstance, rows []*modelv1.SimulatorFill) (adminTradeRelations, error) {
	result := adminTradeRelations{
		orders:    make(map[int64]*modelv1.SimulatorOrder, len(rows)),
		commands:  make(map[int64]*modelv1.SimulatorCommand, len(rows)),
		intents:   make(map[int64]*modelv1.OrderIntent, len(rows)),
		sources:   make(map[int64]domain.MarketSource, len(rows)),
		collected: make(map[string]*modelv1.PortfolioFill, len(rows)),
	}
	ids := make([]int64, 0, len(rows))
	trades := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.CoreOrderId)
		trades = append(trades, row.TradeId)
	}
	var orders []*modelv1.SimulatorOrder
	if err := tx.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Where("id IN ? AND delete_at IS NULL", ids).Find(&orders).Error; err != nil {
		return result, err
	}
	commandIDs := make([]int64, 0, len(orders))
	versions := make([]int64, 0, len(orders))
	for _, row := range orders {
		result.orders[row.Id] = row
		commandIDs = append(commandIDs, row.InitialCommandId)
		versions = append(versions, row.ConfigVersion)
	}
	var commands []*modelv1.SimulatorCommand
	if err := tx.WithContext(ctx).Model(&modelv1.SimulatorCommand{}).Where("id IN ? AND delete_at IS NULL", commandIDs).Find(&commands).Error; err != nil {
		return result, err
	}
	intentIDs := make([]int64, 0, len(commands))
	for _, row := range commands {
		result.commands[row.Id] = row
		if row.IntentId != nil {
			intentIDs = append(intentIDs, *row.IntentId)
		}
	}
	var intents []*modelv1.OrderIntent
	if len(intentIDs) > 0 {
		if err := tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).Where("id IN ? AND delete_at IS NULL", intentIDs).Find(&intents).Error; err != nil {
			return result, err
		}
	}
	for _, row := range intents {
		result.intents[row.Id] = row
	}
	if err := readAdminTradeMetadata(ctx, tx, instance, versions, trades, &result); err != nil {
		return result, err
	}
	return result, nil
}

func readAdminTradeMetadata(ctx context.Context, tx *gorm.DB, instance *modelv1.StrategyInstance, versions []int64, trades []string, result *adminTradeRelations) error {
	var instrument modelv1.Instrument
	if err := tx.WithContext(ctx).Model(&modelv1.Instrument{}).Where("id = ? AND delete_at IS NULL", instance.InstrumentId).Take(&instrument).Error; err != nil {
		return err
	}
	result.quoteAsset = instrument.QuoteAsset
	var configs []*modelv1.StrategyConfigVersion
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).
		Select("Id", "Version", "MarketDataSource").Where("instance_id = ? AND version IN ? AND delete_at IS NULL", instance.Id, versions).Find(&configs).Error; err != nil {
		return err
	}
	for _, row := range configs {
		source := domain.MarketSource(row.MarketDataSource)
		if err := source.ValidateSelection(); err != nil {
			return err
		}
		result.sources[row.Version] = source
	}
	var collected []*modelv1.PortfolioFill
	if err := tx.WithContext(ctx).Model(&modelv1.PortfolioFill{}).
		Where("execution_venue = ? AND trade_id IN ?", domain.ExecutionVenueSimulator, trades).Find(&collected).Error; err != nil {
		return err
	}
	for _, row := range collected {
		result.collected[row.TradeId] = row
	}
	return nil
}

func adminTradeValue(instance *modelv1.StrategyInstance, row *modelv1.SimulatorFill, related adminTradeRelations) (domain.AdminTrade, error) {
	order := related.orders[row.CoreOrderId]
	if order == nil || order.StrategyInstanceId != instance.Id || order.AccountId != instance.AccountId || order.InstrumentId != instance.InstrumentId {
		return domain.AdminTrade{}, domain.ErrAdminUnavailable
	}
	source, found := related.sources[order.ConfigVersion]
	if !found || !validAdminTradeCommand(order, related.commands[order.InitialCommandId]) {
		return domain.AdminTrade{}, domain.ErrAdminUnavailable
	}
	fill, err := coreFill(row)
	if err != nil {
		return domain.AdminTrade{}, err
	}
	core, err := coreOrder(order)
	if err != nil {
		return domain.AdminTrade{}, err
	}
	if !validAdminTradeAmounts(fill, core, related.quoteAsset) || row.Id <= 0 || row.CreateAt <= 0 || row.TradeId == "" {
		return domain.AdminTrade{}, domain.ErrAdminUnavailable
	}
	result := domain.AdminTrade{
		ID:             row.Id,
		ExecutionVenue: domain.ExecutionVenueSimulator,
		TradeID:        row.TradeId,
		StrategyID:     core.StrategyID,
		AccountID:      core.AccountID,
		InstrumentID:   core.InstrumentID,
		ConfigVersion:  core.ConfigVersion,
		Source:         source,
		CoreOrderID:    core.ID,
		ClientOrderID:  core.ClientOrderID,
		Side:           core.Side,
		Price:          fill.Price,
		Quantity:       fill.Quantity,
		QuoteNotional:  fill.QuoteNotional,
		FeeAmount:      fill.FeeAmount,
		FeeAsset:       fill.FeeAsset,
		RecordedAt:     time.UnixMilli(row.CreateAt).UTC(),
	}
	if err := linkAdminTradeOrder(&result, order, related); err != nil {
		return domain.AdminTrade{}, err
	}
	return result, nil
}

func validAdminTradeCommand(order *modelv1.SimulatorOrder, command *modelv1.SimulatorCommand) bool {
	return command != nil && command.Action == domain.GatewayActionSubmit && command.AccountId == order.AccountId &&
		command.StrategyInstanceId == order.StrategyInstanceId && command.ConfigVersion == order.ConfigVersion &&
		command.InstrumentId == order.InstrumentId && command.ClientOrderId == order.ClientOrderId
}

func validAdminTradeAmounts(fill domain.CoreFill, order domain.CoreOrder, quote string) bool {
	if !domain.FixedDecimal(fill.Price, 20, 18, false) || !domain.FixedDecimal(fill.Quantity, 20, 18, false) ||
		!domain.FixedDecimal(fill.QuoteNotional, 40, 36, false) || !domain.FixedDecimal(fill.FeeAmount, 40, 36, true) ||
		!domain.FixedDecimal(order.Price, 20, 18, false) || !domain.FixedDecimal(order.Quantity, 20, 18, false) ||
		!domain.FixedDecimal(order.FilledQuantity, 20, 18, true) || !domain.FixedDecimal(order.QuoteFeeRate, 1, 18, true) ||
		order.QuoteFeeRate.GreaterThanOrEqual(decimal.NewFromInt(1)) || fill.FeeAsset != quote || quote == "" ||
		fill.Quantity.GreaterThan(order.Quantity) || fill.Quantity.GreaterThan(order.FilledQuantity) {
		return false
	}
	if order.Side != domain.SideBuy && order.Side != domain.SideSell ||
		order.Side == domain.SideBuy && fill.Price.GreaterThan(order.Price) || order.Side == domain.SideSell && fill.Price.LessThan(order.Price) {
		return false
	}
	return fill.Price.Mul(fill.Quantity).Equal(fill.QuoteNotional) && fill.QuoteNotional.Mul(order.QuoteFeeRate).Truncate(36).Equal(fill.FeeAmount)
}

func linkAdminTradeOrder(value *domain.AdminTrade, order *modelv1.SimulatorOrder, related adminTradeRelations) error {
	command := related.commands[order.InitialCommandId]
	collected := related.collected[value.TradeID]
	if command.IntentId == nil {
		if collected != nil {
			return domain.ErrAdminUnavailable
		}
		return nil
	}
	row := related.intents[*command.IntentId]
	if row == nil || row.Id <= 0 || row.AccountId != order.AccountId || row.StrategyInstanceId != order.StrategyInstanceId || row.InstrumentId != order.InstrumentId || row.ConfigVersion != order.ConfigVersion || row.ClientOrderId != order.ClientOrderId || row.Side != order.Side || row.TimeInForce != order.TimeInForce || row.ReduceOnly != order.ReduceOnly {
		return domain.ErrAdminUnavailable
	}
	intent, err := intentSnapshot(row)
	if err != nil {
		return err
	}
	if !domain.FixedDecimal(intent.Input.Price, 20, 18, false) || !domain.FixedDecimal(intent.Input.Quantity, 20, 18, false) || !decimalTextEqual(order.Price, intent.Input.Price) || !decimalTextEqual(order.Quantity, intent.Input.Quantity) {
		return domain.ErrAdminUnavailable
	}
	id := row.Id
	value.OrderID = &id
	if collected == nil {
		return nil
	}
	fact := domain.FillFact{
		ExecutionVenue: value.ExecutionVenue,
		TradeID:        value.TradeID,
		SourceSequence: value.ID,
		IntentID:       id,
		CoreOrderID:    value.CoreOrderID,
		AccountID:      value.AccountID,
		StrategyID:     value.StrategyID,
		InstrumentID:   value.InstrumentID,
		Side:           value.Side,
		Price:          value.Price,
		Quantity:       value.Quantity,
		QuoteNotional:  value.QuoteNotional,
		FeeAmount:      value.FeeAmount,
		FeeAsset:       value.FeeAsset,
	}
	if collected.DeleteAt != nil || collected.Id <= 0 || collected.CreateAt <= 0 || !samePortfolioFact(collected, fact) {
		return domain.ErrAdminUnavailable
	}
	value.PortfolioFact = &domain.AdminTradePortfolioFact{
		ID:         collected.Id,
		RecordedAt: time.UnixMilli(collected.CreateAt).UTC(),
	}
	return nil
}
