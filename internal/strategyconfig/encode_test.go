package strategyconfig

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestEncodePreservesEveryTypedFieldAndRejectsMixedOrUnrepresentableParameters(t *testing.T) {
	for _, input := range []*commonv1.StrategyParameters{
		{Template: &commonv1.StrategyParameters_MarketMaker{MarketMaker: makerFixture()}},
		{Template: &commonv1.StrategyParameters_Grid{Grid: gridFixture()}},
	} {
		value, err := Decode(domain.ParametersSchemaVersion, input)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := Encode(value)
		if err != nil || !proto.Equal(encoded, input) {
			t.Fatal("typed decimal field roundtrip changed", err)
		}
	}
	grid, err := Grid(gridFixture())
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []domain.StrategyParameters{
		{},
		{Template: domain.StrategyTemplateGrid},
		{
			Template: domain.StrategyTemplateGrid,
			Grid:     &grid,
			Maker:    &domain.MakerParameters{},
		},
	} {
		if _, err := Encode(value); !errors.Is(err, domain.ErrInvalidParameters) {
			t.Fatal("mixed or missing union accepted", err)
		}
	}
	grid.MaxMarketAge += time.Nanosecond
	if _, err := Encode(domain.StrategyParameters{
		Template: domain.StrategyTemplateGrid,
		Grid:     &grid,
	}); !errors.Is(err, domain.ErrInvalidParameters) {
		t.Fatal("fractional milliseconds silently truncated")
	}
	grid.MaxMarketAge = time.Second
	grid.LowerPrice = decimal.RequireFromString("0.1234567890123456789")
	if _, err := Encode(domain.StrategyParameters{
		Template: domain.StrategyTemplateGrid,
		Grid:     &grid,
	}); !errors.Is(err, domain.ErrInvalidParameters) {
		t.Fatal("unrepresentable wire precision accepted")
	}
}
