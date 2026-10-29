package strategywire

import (
	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategyconfig"
)

// Publication maps only explicit typed fields and rejects unknown parameters.
func Publication(input *strategyv1.PublishConfigurationInput, strategyID int64, key string) (domain.AdminPublishStrategy, error) {
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 {
		return domain.AdminPublishStrategy{}, domain.ErrInvalidAdminPublication
	}
	parameters, err := strategyconfig.Decode(domain.ParametersSchemaVersion, input.Parameters)
	if err != nil {
		return domain.AdminPublishStrategy{}, domain.ErrInvalidAdminPublication
	}
	return (domain.AdminPublishStrategy{
		Key:                   key,
		StrategyID:            strategyID,
		ExpectedRevision:      input.ExpectedRevision,
		ExpectedConfigVersion: input.ExpectedConfigVersion,
		Source:                domain.MarketSource(input.MarketDataSource),
		Parameters:            parameters,
		Reason:                input.Reason,
	}).Normalize()
}
