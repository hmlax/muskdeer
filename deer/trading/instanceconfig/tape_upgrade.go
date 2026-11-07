package instanceconfig

import (
	"context"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	config "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	commonmodel "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	model "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

type tapeUpgrade struct {
	row        *model.StrategyInstance
	write      *common.StrategyInstanceInput
	connection *config.VenueConnection
}

// An explicit offline removal of just the two quotas can continue the original
// run. Preserve its audit identity and bind it to the exact new parameters.
// Any other consumed-run parameter change still fails the final Load check.
func preserveUnlimitedTape(ctx context.Context, tx *gorm.DB, change tapeUpgrade) error {
	row, write := change.row, change.write
	previous := proto.CloneOf(row)
	row.TapePolicyDigest, row.TapeParametersDigest = "", ""
	if !unlimitedTapeUpgrade(previous, write) {
		return nil
	}
	var instrument commonmodel.Instrument
	if err := fresh(ctx, tx).Model(&commonmodel.Instrument{}).Where("id = ?", previous.InstrumentId).Take(&instrument).Error; err != nil {
		return err
	}
	var consumed int64
	if err := fresh(ctx, tx).Model(&execution.SyntheticTapeIntent{}).
		Where("run_id = ? AND symbol = ?", previous.Parameters.WashTrade.RunId, instrument.Symbol).Count(&consumed).Error; err != nil {
		return err
	}
	if consumed == 0 {
		return nil
	}
	var budget commonmodel.TradingAccountPolicy
	var template model.StrategyTemplate
	if err := fresh(ctx, tx).Model(&commonmodel.TradingAccountPolicy{}).Where("account_id = ?", previous.AccountId).Take(&budget).Error; err != nil {
		return err
	}
	if err := fresh(ctx, tx).Model(&model.StrategyTemplate{}).Where("id = ?", previous.TemplateId).Take(&template).Error; err != nil {
		return err
	}
	instance, err := fromRow(previous, &template, &instrument, &budget)
	if err != nil {
		return err
	}
	settings, err := instance.Settings()
	if err != nil {
		return err
	}
	if change.connection != nil {
		settings.Venue.Target, settings.Venue.TokenEnv = change.connection.Target, change.connection.TokenEnv
	}
	row.TapePolicyDigest, err = native.SyntheticPolicyDigest(settings.Venue)
	if err != nil {
		return err
	}
	row.TapeParametersDigest, err = ParametersDigest(write.Parameters)
	return err
}

func unlimitedTapeUpgrade(row *model.StrategyInstance, write *common.StrategyInstanceInput) bool {
	if row.Template != string(domain.StrategyTemplateWashTrade) || row.Parameters.GetWashTrade() == nil || write.Parameters.GetWashTrade() == nil ||
		row.TemplateId != write.TemplateId || row.TemplateRevision != write.TemplateRevision || row.MarketDataSource != write.MarketDataSource || row.Implementation != write.Implementation {
		return false
	}
	expected := proto.CloneOf(row.Parameters)
	expected.WashTrade.MaximumEvents = 0
	expected.WashTrade.MaximumNotional = "0"
	return proto.Equal(expected, write.Parameters)
}
