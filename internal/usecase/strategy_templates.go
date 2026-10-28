package usecase

import (
	"time"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/quoting"
)

const gridMinimumRefresh = 100 * time.Millisecond

type strategyQuotes struct {
	targets    []quoting.Target
	reprice    decimal.Decimal
	minRefresh time.Duration
	maxAge     time.Duration
}

func strategyQuoteTargets(spec domain.StrategySpec, environment domain.StrategyEnvironmentView, reference domain.MarketReference, now time.Time) (strategyQuotes, error) {
	var result strategyQuotes
	parameters := environment.Parameters
	if spec.Template != parameters.Template {
		return result, domain.ErrInvalidParameters
	}
	input := quoting.Input{
		Instrument: quoting.Instrument{
			ID:              environment.Instrument.ID,
			MetadataVersion: environment.Instrument.MetadataVersion,
			Status:          environment.Instrument.Status,
			PriceTick:       environment.Instrument.PriceTick,
			QuantityStep:    environment.Instrument.QuantityStep,
			MinNotional:     environment.Instrument.MinNotional,
		},
		Source:         spec.ActiveSource,
		Symbol:         environment.Symbol,
		Mode:           spec.Mode,
		Market:         reference,
		Inventory:      environment.Inventory,
		AvailableBase:  environment.AvailableBase,
		AvailableQuote: environment.AvailableQuote,
		QuoteFeeRate:   environment.QuoteFeeRate,
		LocalBid:       environment.LocalBid,
		LocalAsk:       environment.LocalAsk,
		Now:            now,
	}
	var err error
	switch parameters.Template {
	case domain.StrategyTemplateMarketMaker:
		if parameters.Maker == nil || parameters.Grid != nil {
			return result, domain.ErrInvalidParameters
		}
		input.Parameters = *parameters.Maker
		result.reprice, result.minRefresh, result.maxAge = parameters.Maker.RepriceBPS, parameters.Maker.MinRefreshInterval, parameters.Maker.MaxQuoteAge
		result.targets, err = quoting.Maker(input)
	case domain.StrategyTemplateGrid:
		if parameters.Grid == nil || parameters.Maker != nil {
			return result, domain.ErrInvalidParameters
		}
		// A fixed level only matches its exact price. Keep safe unchanged orders
		// across ticks; the one-hour maintenance bound still applies. Protection
		// bypasses throttling and all final execution gates remain authoritative.
		result.reprice, result.minRefresh, result.maxAge = decimal.Zero, gridMinimumRefresh, time.Hour
		result.targets, err = quoting.Grid(quoting.GridInput{
			Instrument:     input.Instrument,
			Source:         input.Source,
			Symbol:         input.Symbol,
			Mode:           input.Mode,
			Market:         input.Market,
			Parameters:     *parameters.Grid,
			Inventory:      input.Inventory,
			AvailableBase:  input.AvailableBase,
			AvailableQuote: input.AvailableQuote,
			QuoteFeeRate:   input.QuoteFeeRate,
			LocalBid:       input.LocalBid,
			LocalAsk:       input.LocalAsk,
			Now:            input.Now,
		})
	default:
		return result, domain.ErrInvalidParameters
	}
	return result, err
}
