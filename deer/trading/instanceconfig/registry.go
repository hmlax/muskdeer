// Package instanceconfig loads immutable startup plans from strategy_instances.
// It never selects a second parameter source or starts business workers.
package instanceconfig

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/strategyconfig"
	"github.com/hmlax/muskdeer/deer/trading/strategyregistry"
)

// Parameter schema and implementation identities are stored on each instance.
const (
	ParametersSchemaVersion = strategyregistry.ParametersSchemaVersion
	ImplementationMaker     = strategyregistry.ImplementationMaker
	ImplementationGrid      = strategyregistry.ImplementationGrid
	ImplementationWashTrade = strategyregistry.ImplementationWashTrade
	messageInvalidInstance  = "strategy instance configuration is missing or invalid"
)

// ErrInvalid rejects incomplete or mismatched instance configuration.
var ErrInvalid = errors.New(messageInvalidInstance)

// DataRequirements names the inputs consumed by an implementation.
type DataRequirements struct {
	OrderBook bool
	Account   bool
	Trades    bool
}

// Definition identifies compiled implementations available to database templates.
type Definition struct {
	Type           domain.StrategyTemplate
	Implementation string
}

// Definitions returns the three registered algorithms without mutable globals.
func Definitions() []Definition {
	return []Definition{
		{
			Type:           domain.StrategyTemplateMarketMaker,
			Implementation: ImplementationMaker,
		},
		{
			Type:           domain.StrategyTemplateGrid,
			Implementation: ImplementationGrid,
		},
		{
			Type:           domain.StrategyTemplateWashTrade,
			Implementation: ImplementationWashTrade,
		},
	}
}

// Validate requires a matching implementation and exactly one concrete parameter set.
func Validate(kind domain.StrategyTemplate, implementation string, p *commonv1.StrategyInstanceParameters) error {
	if p == nil || p.SchemaVersion != ParametersSchemaVersion || p.ManagementChange != nil || !validExecution(p.Execution) {
		return ErrInvalid
	}
	switch kind {
	case domain.StrategyTemplateMarketMaker:
		if implementation != ImplementationMaker || p.MarketMaker == nil || p.Grid != nil || p.WashTrade != nil || !validMaker(p.MarketMaker) {
			return ErrInvalid
		}
	case domain.StrategyTemplateGrid:
		if implementation != ImplementationGrid || p.Grid == nil || p.MarketMaker != nil || p.WashTrade != nil {
			return ErrInvalid
		}
		_, err := strategyconfig.Decode(domain.ParametersSchemaVersion, &commonv1.StrategyParameters{
			Template: &commonv1.StrategyParameters_Grid{Grid: p.Grid},
		})
		return err
	case domain.StrategyTemplateWashTrade:
		if implementation != ImplementationWashTrade || p.WashTrade == nil || p.MarketMaker != nil || p.Grid != nil || !validWashTrade(p.WashTrade) {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

// Requirements is derived from the selected algorithm and its concrete mode.
func Requirements(kind domain.StrategyTemplate, p *commonv1.StrategyInstanceParameters) DataRequirements {
	return DataRequirements{
		OrderBook: true,
		Account:   kind != domain.StrategyTemplateWashTrade,
		Trades:    kind == domain.StrategyTemplateWashTrade && p.GetWashTrade().GetMode() == commonv1.WashTradeMode_WASH_TRADE_MODE_AGG_TRADE,
	}
}

// ParametersDigest binds migration metadata to the exact typed current parameters.
func ParametersDigest(p *commonv1.StrategyInstanceParameters) (string, error) {
	if p == nil {
		return "", ErrInvalid
	}
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(p)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

func validExecution(p *commonv1.StrategyExecutionParameters) bool {
	return p != nil && p.PollIntervalMs >= 10 && p.PollIntervalMs <= 5000 &&
		p.OperationTimeoutMs >= 100 && p.OperationTimeoutMs <= 5000 &&
		p.LeaseTtlMs >= 3*p.OperationTimeoutMs && p.LeaseTtlMs <= domain.MaxLeaseTTL.Milliseconds() &&
		p.Leverage > 0 && p.Leverage <= 10
}

func validMaker(p *commonv1.DepthMakerParameters) bool {
	fraction, e1 := native.Value(p.QuantityFraction, false, false)
	replacement, e2 := native.Value(p.ReplacementFraction, false, false)
	return e1 == nil && e2 == nil && fraction.LessThanOrEqual(decimal.NewFromInt(1)) && replacement.LessThanOrEqual(decimal.NewFromInt(1)) &&
		p.Depth > 0 && p.Depth <= 20 && p.BridgeSlots <= p.Depth
}

func validWashTrade(p *commonv1.WashTradeParameters) bool {
	if !p.DevelopmentOnly || p.IntervalMs < 100 || p.IntervalMs > time.Minute.Milliseconds() || len(p.RunId) == 0 || len(p.RunId) > 20 {
		return false
	}
	for _, character := range p.RunId {
		if character != '-' && (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	if p.Mode != commonv1.WashTradeMode_WASH_TRADE_MODE_BOOK && p.Mode != commonv1.WashTradeMode_WASH_TRADE_MODE_AGG_TRADE {
		return false
	}
	_, e1 := native.Value(p.Quantity, false, false)
	_, e2 := native.Value(p.MaximumNotional, false, true)
	fraction, e3 := native.Value(p.QuantityFraction, false, false)
	return e1 == nil && e2 == nil && e3 == nil && fraction.LessThanOrEqual(decimal.NewFromInt(1))
}
