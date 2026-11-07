package instancemove

import (
	"context"
	"math"
	"strings"
	"time"

	config "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	values "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	"github.com/hmlax/muskdeer/deer/trading/strategyregistry"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
)

const (
	historySchemaVersion = 3
	migrationCreator     = "instance-schema-migration"
)

func convertInstances(ctx context.Context, db *gorm.DB) ([]*strategy.StrategyInstance, []*strategy.StrategyConfigVersion, error) {
	var originals []*strategy.StrategyInstance
	if err := fresh(ctx, db).Model(&strategy.StrategyInstance{}).Order("id").Find(&originals).Error; err != nil {
		return nil, nil, err
	}
	var oldVersions []*strategy.StrategyConfigVersion
	if err := fresh(ctx, db).Model(&strategy.StrategyConfigVersion{}).Order("id").Find(&oldVersions).Error; err != nil {
		return nil, nil, err
	}
	byKey := make(map[[2]int64]*strategy.StrategyConfigVersion)
	var maxVersionID, maxInstanceID int64
	for _, version := range oldVersions {
		byKey[[2]int64{
			version.InstanceId,
			version.Version,
		}] = version
		maxVersionID = max(maxVersionID, version.Id)
	}
	for _, row := range originals {
		maxInstanceID = max(maxInstanceID, row.Id)
	}
	instanceSequence, err := sequenceHighWater(ctx, db, &strategy.StrategyInstance{})
	if err != nil {
		return nil, nil, err
	}
	versionSequence, err := sequenceHighWater(ctx, db, &strategy.StrategyConfigVersion{})
	if err != nil {
		return nil, nil, err
	}
	maxInstanceID = max(maxInstanceID, instanceSequence.Int64)
	maxVersionID = max(maxVersionID, versionSequence.Int64)
	var converted []*strategy.StrategyInstance
	var versions []*strategy.StrategyConfigVersion
	for _, original := range originals {
		row := proto.CloneOf(original)
		previous := byKey[[2]int64{
			row.Id,
			row.RequestedConfigVersion,
		}]
		if row.Template != string(domain.StrategyTemplateMarketMaker) || previous == nil || previous.ParametersSchemaVersion != 2 || previous.MakerParameters.GetVenueRuntime() == nil || row.Revision == math.MaxInt64 || row.RequestedConfigVersion == math.MaxInt64 {
			return nil, nil, ErrSource
		}
		runtime := previous.MakerParameters.VenueRuntime
		var budget common.TradingAccountPolicy
		if err := fresh(ctx, db).Model(&common.TradingAccountPolicy{}).Where("account_id = ?", row.AccountId).Take(&budget).Error; err != nil {
			return nil, nil, err
		}
		p := makerParameters(runtime, budget.Leverage)
		if err := instanceconfig.Validate(domain.StrategyTemplateMarketMaker, instanceconfig.ImplementationMaker, p); err != nil {
			return nil, nil, err
		}
		row.Parameters = p
		row.Implementation = instanceconfig.ImplementationMaker
		row.TemplateId, row.TemplateRevision = strategyregistry.MakerTemplateID, 1
		row.Revision++
		row.RequestedConfigVersion++
		row.UpdateAt = time.Now().UnixMilli()
		if row.DesiredRunning {
			row.DesiredState = string(domain.StrategyStateRunning)
		}
		converted = append(converted, row)
		maxVersionID++
		versions = append(versions, currentHistory(row, maxVersionID))
		tape := runtime.Venue.GetSyntheticTape()
		if !tape.GetEnabled() {
			continue
		}
		if len(tape.Profiles) != 1 || len(runtime.Venue.Symbols) != 1 || tape.Profiles[0].Symbol != runtime.Venue.Symbols[0] || budget.Symbol != tape.Profiles[0].Symbol {
			return nil, nil, ErrSource
		}
		maxInstanceID++
		wash := proto.CloneOf(row)
		wash.Id = maxInstanceID
		wash.InstanceKey = strings.Replace(row.InstanceKey, "venue/", "wash/", 1)
		if wash.InstanceKey == row.InstanceKey {
			wash.InstanceKey = "wash/" + budget.Symbol
		}
		wash.Template = string(domain.StrategyTemplateWashTrade)
		wash.Implementation = instanceconfig.ImplementationWashTrade
		wash.TemplateId, wash.TemplateRevision = strategyregistry.WashTradeTemplateID, 1
		wash.Revision, wash.RequestedConfigVersion = 1, 1
		wash.ActiveConfigVersion, wash.ActiveMarketDataSource = nil, nil
		wash.CreateAt, wash.UpdateAt = time.Now().UnixMilli(), time.Now().UnixMilli()
		wash.RuntimeOwnerId, wash.RuntimeOwnerEpoch = "", 0
		wash.Parameters = &values.StrategyInstanceParameters{
			SchemaVersion: instanceconfig.ParametersSchemaVersion,
			Execution:     proto.CloneOf(p.Execution),
			WashTrade: &values.WashTradeParameters{
				DevelopmentOnly:  tape.DevelopmentOnly,
				Mode:             values.WashTradeMode_WASH_TRADE_MODE_BOOK,
				RunId:            tape.RunId,
				IntervalMs:       tape.Interval.AsDuration().Milliseconds(),
				MaximumEvents:    tape.MaximumEvents,
				Quantity:         tape.Profiles[0].Quantity,
				MaximumNotional:  tape.Profiles[0].MaximumNotional,
				QuantityFraction: runtime.Venue.QuantityFraction,
			},
		}
		if tape.Mode == config.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE {
			wash.Parameters.WashTrade.Mode = values.WashTradeMode_WASH_TRADE_MODE_AGG_TRADE
		}
		if err := instanceconfig.Validate(domain.StrategyTemplateWashTrade, wash.Implementation, wash.Parameters); err != nil {
			return nil, nil, err
		}
		var run struct {
			Count    int64
			Policies int64
			Digest   string
		}
		if err := fresh(ctx, db).Model(&execution.SyntheticTapeIntent{}).Where("run_id = ? AND symbol = ?", tape.RunId, budget.Symbol).
			Select("count(*) AS count,count(DISTINCT policy_digest) AS policies,coalesce(min(policy_digest),'') AS digest").Scan(&run).Error; err != nil {
			return nil, nil, err
		}
		if run.Count > 0 {
			if run.Policies != 1 {
				return nil, nil, ErrSource
			}
			wash.TapePolicyDigest = run.Digest
			digest, err := instanceconfig.ParametersDigest(wash.Parameters)
			if err != nil {
				return nil, nil, err
			}
			wash.TapeParametersDigest = digest
		}
		converted = append(converted, wash)
		maxVersionID++
		versions = append(versions, currentHistory(wash, maxVersionID))
	}
	return converted, versions, nil
}
func makerParameters(runtime *config.BusinessRuntime, leverage uint32) *values.StrategyInstanceParameters {
	return &values.StrategyInstanceParameters{
		SchemaVersion: instanceconfig.ParametersSchemaVersion,
		Execution: &values.StrategyExecutionParameters{
			PollIntervalMs:     runtime.GetPollInterval().AsDuration().Milliseconds(),
			OperationTimeoutMs: runtime.GetOperationTimeout().AsDuration().Milliseconds(),
			LeaseTtlMs:         runtime.GetVenue().GetLeaseTtl().AsDuration().Milliseconds(),
			Leverage:           leverage,
		},
		MarketMaker: &values.DepthMakerParameters{
			Depth:               runtime.GetVenue().GetDepth(),
			QuantityFraction:    runtime.GetVenue().GetQuantityFraction(),
			ReplacementFraction: runtime.GetVenue().GetReplacementFraction(),
			BridgeSlots:         runtime.GetVenue().GetBridgeSlots(),
		},
	}
}
func currentHistory(row *strategy.StrategyInstance, id int64) *strategy.StrategyConfigVersion {
	kind := row.Template
	return &strategy.StrategyConfigVersion{
		Id:                      id,
		InstanceId:              row.Id,
		TemplateId:              &row.TemplateId,
		TemplateRevision:        &row.TemplateRevision,
		Version:                 row.RequestedConfigVersion,
		MarketDataSource:        row.MarketDataSource,
		CodeVersion:             row.CodeVersion,
		CreatedBy:               migrationCreator,
		ParametersSchemaVersion: historySchemaVersion,
		Template:                &kind,
		Parameters:              proto.CloneOf(row.Parameters),
	}
}
