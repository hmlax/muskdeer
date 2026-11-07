package instanceconfig

import (
	"context"
	"database/sql"
	"encoding/hex"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	commonmodelv1 "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	strategymodelv1 "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/strategyregistry"
)

// Instance is a private, typed startup snapshot, not another persistence model.
type Instance struct {
	TemplateID       int64
	TemplateRevision int64
	RequiredInputs   *commonv1.StrategyTemplateInputs
	ID               int64
	Revision         int64
	Key              string
	Type             domain.StrategyTemplate
	Implementation   string
	Source           domain.MarketSource
	Profile          *commonv1.VenueProfilePolicy
	Parameters       *commonv1.StrategyInstanceParameters
	TapePolicyDigest string
	ResumeSequence   uint64
}

// Load reads every requested instance from one repeatable-read startup snapshot.
// Missing current parameters never fall back to strategy_config_versions.
// A supplied connection binds consumed tape identity to the same transport
// fields that execution attaches before publication. It supplies no parameters.
func Load(ctx context.Context, db *gorm.DB, connections ...*configv1.VenueConnection) ([]*Instance, error) {
	return load(ctx, db, false, connections...)
}

// LoadAll assembles stopped instances as well, enabling explicit control without hot parameter reload.
func LoadAll(ctx context.Context, db *gorm.DB, connections ...*configv1.VenueConnection) ([]*Instance, error) {
	return load(ctx, db, true, connections...)
}

func load(ctx context.Context, db *gorm.DB, all bool, connections ...*configv1.VenueConnection) ([]*Instance, error) {
	if err := validContext(ctx); err != nil || db == nil {
		return nil, ErrInvalid
	}
	if len(connections) > 1 {
		return nil, ErrInvalid
	}
	var connection *configv1.VenueConnection
	if len(connections) == 1 {
		if connections[0].GetTarget() == "" || connections[0].GetTokenEnv() == "" {
			return nil, ErrInvalid
		}
		connection = proto.CloneOf(connections[0])
	}
	var result []*Instance
	err := fresh(ctx, db).Transaction(func(tx *gorm.DB) error {
		var rows []*strategymodelv1.StrategyInstance
		if err := fresh(ctx, tx).Model(&strategymodelv1.StrategyInstance{}).Where("delete_at IS NULL").Order("id").Find(&rows).Error; err != nil {
			return err
		}
		templates, err := loadTemplates(ctx, tx)
		if err != nil {
			return err
		}
		instruments, budgets, err := loadBindings(ctx, tx)
		if err != nil {
			return err
		}
		runs := make(map[string]bool)
		for _, row := range rows {
			instance, err := fromRow(row, templates[row.TemplateId], instruments[row.InstrumentId], budgets[row.AccountId])
			if err != nil {
				return err
			}
			if !all && !row.DesiredRunning {
				continue
			}
			if instance.Type == domain.StrategyTemplateWashTrade {
				key := native.SyntheticExecutionKey(instance.Parameters.WashTrade.RunId, instance.Profile.Symbol)
				if runs[key] {
					return ErrInvalid
				}
				runs[key] = true
				if err := resumeTape(ctx, tx, instance, connection); err != nil {
					return err
				}
			}
			result = append(result, instance)
		}
		return nil
	}, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	return result, err
}

func loadBindings(ctx context.Context, db *gorm.DB) (map[int64]*commonmodelv1.Instrument, map[string]*commonmodelv1.TradingAccountPolicy, error) {
	var instruments []*commonmodelv1.Instrument
	var budgets []*commonmodelv1.TradingAccountPolicy
	instrumentIDs := fresh(ctx, db).Model(&strategymodelv1.StrategyInstance{}).Where("delete_at IS NULL").Select("instrument_id")
	if err := fresh(ctx, db).Model(&commonmodelv1.Instrument{}).Where("id IN (?) AND delete_at IS NULL", instrumentIDs).Find(&instruments).Error; err != nil {
		return nil, nil, err
	}
	accountIDs := fresh(ctx, db).Model(&strategymodelv1.StrategyInstance{}).Where("delete_at IS NULL").Select("account_id")
	if err := fresh(ctx, db).Model(&commonmodelv1.TradingAccountPolicy{}).Where("account_id IN (?) AND delete_at IS NULL", accountIDs).Find(&budgets).Error; err != nil {
		return nil, nil, err
	}
	byInstrument := make(map[int64]*commonmodelv1.Instrument, len(instruments))
	byAccount := make(map[string]*commonmodelv1.TradingAccountPolicy, len(budgets))
	for _, row := range instruments {
		byInstrument[row.Id] = row
	}
	for _, row := range budgets {
		byAccount[row.AccountId] = row
	}
	return byInstrument, byAccount, nil
}

func fromRow(row *strategymodelv1.StrategyInstance, template *strategymodelv1.StrategyTemplate, instrument *commonmodelv1.Instrument, budget *commonmodelv1.TradingAccountPolicy) (*Instance, error) {
	if row == nil || !validTemplateBinding(row, template) || row.Id <= 0 || row.Revision <= 0 || row.InstanceKey == "" || instrument == nil || budget == nil ||
		instrument.ExecutionVenue != native.Venue || instrument.Status != domain.InstrumentStatusTrading ||
		Validate(domain.StrategyTemplate(row.Template), row.Implementation, row.Parameters) != nil {
		return nil, ErrInvalid
	}
	if domain.MarketSource(row.MarketDataSource).ValidateSelection() != nil {
		return nil, ErrInvalid
	}
	profile := &commonv1.VenueProfilePolicy{
		UserId:               budget.UserId,
		FundId:               budget.FundId,
		Symbol:               instrument.Symbol,
		MaximumNotional:      decimalText(budget.MaximumNotional),
		MaximumOrderNotional: decimalText(budget.MaximumOrderNotional),
		Leverage:             row.Parameters.Execution.Leverage,
		PriceTick:            decimalText(instrument.PriceTick),
		QuantityStep:         decimalText(instrument.QuantityStep),
	}
	if budget.DeleteAt != nil || budget.AccountId != native.ScopeKey(budget.UserId, budget.FundId) || budget.Symbol != instrument.Symbol || profile.Leverage != budget.Leverage {
		return nil, ErrInvalid
	}
	profile.MarketDataSource = row.MarketDataSource
	profile.StrategyInstanceId = row.Id
	if !native.ValidProfile(profile) {
		return nil, ErrInvalid
	}
	if row.TapePolicyDigest != "" || row.TapeParametersDigest != "" {
		digest, err := ParametersDigest(row.Parameters)
		if err != nil || row.Template != string(domain.StrategyTemplateWashTrade) || !validDigest(row.TapePolicyDigest) || row.TapeParametersDigest != digest {
			return nil, ErrInvalid
		}
	}
	return &Instance{
		ID:               row.Id,
		TemplateID:       row.TemplateId,
		TemplateRevision: row.TemplateRevision,
		RequiredInputs:   proto.CloneOf(template.RequiredInputs),
		Revision:         row.Revision,
		Key:              row.InstanceKey,
		Type:             domain.StrategyTemplate(row.Template),
		Implementation:   row.Implementation,
		Source:           domain.MarketSource(row.MarketDataSource),
		Profile:          profile,
		Parameters:       proto.CloneOf(row.Parameters),
		TapePolicyDigest: row.TapePolicyDigest,
	}, nil
}

func resumeTape(ctx context.Context, db *gorm.DB, instance *Instance, connection *configv1.VenueConnection) error {
	p := instance.Parameters.WashTrade
	var state struct {
		Count    int64
		First    int64
		Last     int64
		Policies int64
		Digest   string
	}
	if err := fresh(ctx, db).Model(&executionmodelv1.SyntheticTapeIntent{}).Where("run_id = ? AND symbol = ?", p.RunId, instance.Profile.Symbol).
		Select("count(*) AS count, coalesce(min(sequence),0) AS first, coalesce(max(sequence),-1) AS last, count(DISTINCT policy_digest) AS policies, coalesce(min(policy_digest),'') AS digest").Scan(&state).Error; err != nil {
		return err
	}
	if state.Count < 0 || p.MaximumEvents > 0 && state.Count > int64(p.MaximumEvents) || state.Last+1 != state.Count || state.Count > 0 && (state.First != 0 || state.Policies != 1) {
		return ErrInvalid
	}
	if state.Count > 0 {
		settings, err := instance.Settings()
		if err != nil {
			return err
		}
		if connection != nil {
			settings.Venue.Target, settings.Venue.TokenEnv = connection.Target, connection.TokenEnv
		}
		digest, err := native.SyntheticPolicyDigest(settings.Venue)
		if err != nil || state.Digest != digest {
			return ErrInvalid
		}
	} else if instance.TapePolicyDigest != "" {
		return ErrInvalid
	}
	instance.ResumeSequence = uint64(state.Count)
	return nil
}

func decimalText(value string) string {
	parsed, err := domain.ParseLedgerDecimal(value, false, false)
	if err != nil {
		return ""
	}
	return parsed.String()
}

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && len(value) == 64
}

func validContext(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrInvalid
	}
	return nil
}

func fresh(ctx context.Context, db *gorm.DB) *gorm.DB {
	return db.Session(&gorm.Session{
		NewDB:   true,
		Context: ctx,
	})
}

func loadTemplates(ctx context.Context, db *gorm.DB) (map[int64]*strategymodelv1.StrategyTemplate, error) {
	var rows []*strategymodelv1.StrategyTemplate
	if err := fresh(ctx, db).Model(&strategymodelv1.StrategyTemplate{}).Where("delete_at IS NULL").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make(map[int64]*strategymodelv1.StrategyTemplate, len(rows))
	for _, row := range rows {
		if !strategyregistry.Matches(row) {
			return nil, ErrInvalid
		}
		result[row.Id] = row
	}
	return result, nil
}
func validTemplateBinding(row *strategymodelv1.StrategyInstance, template *strategymodelv1.StrategyTemplate) bool {
	return row != nil && template != nil && row.TemplateId > 0 && row.TemplateRevision == template.Revision && row.Template == template.StrategyType && row.Implementation == template.Implementation && (!row.DesiredRunning || template.Enabled)
}

// Requirements returns the selected database template's resolved data inputs.
func (i *Instance) Requirements() DataRequirements {
	if i == nil || i.RequiredInputs == nil {
		return DataRequirements{}
	}
	return DataRequirements{
		OrderBook: i.RequiredInputs.OrderBook,
		Account:   i.RequiredInputs.Account,
		Trades:    i.RequiredInputs.TradesWhenWashMode != commonv1.WashTradeMode_WASH_TRADE_MODE_UNSPECIFIED && i.RequiredInputs.TradesWhenWashMode == i.Parameters.GetWashTrade().GetMode(),
	}
}
