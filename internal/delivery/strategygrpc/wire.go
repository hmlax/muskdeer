package strategygrpc

import (
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategyconfig"
)

func strategyProto(value domain.AdminStrategy) *strategyv1.Strategy {
	spec := value.Spec
	result := &strategyv1.Strategy{
		Id:                        spec.ID,
		InstanceKey:               spec.Key,
		AccountId:                 spec.AccountID,
		InstrumentId:              spec.InstrumentID,
		Template:                  string(spec.Template),
		CodeVersion:               spec.CodeVersion,
		Revision:                  spec.Revision,
		State:                     string(spec.State),
		DesiredState:              string(spec.DesiredState),
		RequestedConfigVersion:    spec.RequestedVersion,
		RequestedMarketDataSource: string(spec.RequestedSource),
		MarketMode:                string(spec.Mode),
		RuntimeOwnerId:            spec.RuntimeOwnerID,
		RuntimeOwnerEpoch:         spec.RuntimeOwnerEpoch,
		CreatedAtMs:               value.CreatedAt.UnixMilli(),
		UpdatedAtMs:               value.UpdatedAt.UnixMilli(),
	}
	if spec.ActiveVersion > 0 {
		result.ActiveConfigVersion = proto.Int64(spec.ActiveVersion)
		result.ActiveMarketDataSource = proto.String(string(spec.ActiveSource))
	}
	return result
}

func configProto(value domain.AdminConfigVersion) (*strategyv1.ConfigVersion, error) {
	result := &strategyv1.ConfigVersion{
		Id:                      value.ID,
		StrategyId:              value.StrategyID,
		Version:                 value.Version,
		MarketDataSource:        string(value.Source),
		CodeVersion:             value.CodeVersion,
		CreatedBy:               value.CreatedBy,
		ParametersSchemaVersion: value.SchemaVersion,
		CreatedAtMs:             value.CreatedAt.UnixMilli(),
	}
	if value.SchemaVersion == 0 && value.Parameters.Template == "" && value.Parameters.Maker == nil && value.Parameters.Grid == nil {
		return result, nil
	}
	if value.SchemaVersion != domain.ParametersSchemaVersion {
		return nil, domain.ErrAdminUnavailable
	}
	var err error
	result.Parameters, err = strategyconfig.Encode(value.Parameters)
	return result, err
}
