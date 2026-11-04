package repository

import (
	"errors"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	strategymodelv1 "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func TestAdminConfigLegacyHasNoInventedParametersAndMalformedDataFailsClosed(t *testing.T) {
	instance := &strategymodelv1.StrategyInstance{Template: string(domain.StrategyTemplateGrid)}
	legacy := &strategymodelv1.StrategyConfigVersion{
		InstanceId:       1,
		Version:          1,
		MarketDataSource: string(domain.MarketSourceOKX),
	}
	value, err := adminConfigVersion(legacy, instance)
	if err != nil || value.SchemaVersion != 0 || value.Parameters.Template != "" || value.Parameters.Maker != nil || value.Parameters.Grid != nil {
		t.Fatal("legacy metadata became a runnable template", err)
	}
	for _, invalid := range []*strategymodelv1.StrategyConfigVersion{
		{
			ParametersSchemaVersion: domain.ParametersSchemaVersion,
			Template:                proto.String(string(domain.StrategyTemplateGrid)),
		},
		{
			ParametersSchemaVersion: domain.ParametersSchemaVersion,
			Template:                proto.String(string(domain.StrategyTemplateGrid)),
			GridParameters:          &commonv1.GridParameters{LowerPrice: "invalid-private-data"},
		},
		{
			ParametersSchemaVersion: domain.ParametersSchemaVersion,
			Template:                proto.String(string(domain.StrategyTemplateMarketMaker)),
			MakerParameters:         &commonv1.MakerParameters{},
		},
		{
			ParametersSchemaVersion: 2,
			Template:                proto.String(string(domain.StrategyTemplateGrid)),
		},
	} {
		_, err := adminConfigVersion(invalid, instance)
		public := adminQueryFailure(err)
		if !errors.Is(public, domain.ErrAdminUnavailable) || public.Error() != domain.MessageAdminUnavailable || strings.Contains(public.Error(), "private-data") {
			t.Fatal("malformed persistence became parameters or leaked details")
		}
	}
}
