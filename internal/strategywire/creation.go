// Package strategywire maps concrete management messages to validated commands.
package strategywire

import (
	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategyconfig"
)

// Creation rejects unknown input/parameter fields before any business storage.
func Creation(input *strategyv1.CreateStrategyInput, key string) (domain.AdminCreateStrategy, error) {
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 {
		return domain.AdminCreateStrategy{}, domain.ErrInvalidAdminCreation
	}
	parameters, err := strategyconfig.Decode(domain.ParametersSchemaVersion, input.Parameters)
	if err != nil {
		return domain.AdminCreateStrategy{}, domain.ErrInvalidAdminCreation
	}
	return (domain.AdminCreateStrategy{
		Key:          key,
		InstanceKey:  input.InstanceKey,
		AccountID:    input.AccountId,
		InstrumentID: input.InstrumentId,
		CodeVersion:  input.CodeVersion,
		Source:       domain.MarketSource(input.MarketDataSource),
		Mode:         domain.MarketMode(input.MarketMode),
		Parameters:   parameters,
		Reason:       input.Reason,
	}).Normalize()
}
