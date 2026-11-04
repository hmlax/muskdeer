package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	commonmodelv1 "github.com/hmlax/muskdeer/deer/proto/common/model/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/proto/execution/model/v1"
	marketmodelv1 "github.com/hmlax/muskdeer/deer/proto/market/model/v1"
	portfoliomodelv1 "github.com/hmlax/muskdeer/deer/proto/portfolio/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func lockCreationDependencies(ctx context.Context, tx *gorm.DB, request domain.AdminCreateStrategy) error {
	// Match core's instrument-before-account order. Publication already holds
	// its strategy row; dependency reads remain shared until commit.
	var instrument commonmodelv1.Instrument
	if err := tx.WithContext(ctx).Model(&commonmodelv1.Instrument{}).Where("id = ? AND delete_at IS NULL", request.InstrumentID).
		Clauses(clause.Locking{Strength: "SHARE"}).Take(&instrument).Error; err != nil {
		return creationDependencyFailure(err)
	}
	if instrument.ExecutionVenue != domain.ExecutionVenueSimulator || instrument.Status != domain.InstrumentStatusTrading {
		return domain.ErrInvalidAdminCreation
	}
	rules, err := strategyInstrumentValue(&instrument)
	if err != nil {
		return domain.ErrInvalidAdminCreation
	}
	orderNotional := request.Parameters.Maker
	if request.Parameters.Grid != nil {
		if request.Parameters.Grid.PerOrderNotional.LessThan(rules.MinNotional) {
			return domain.ErrInvalidAdminCreation
		}
	} else if orderNotional.LevelNotional.LessThan(rules.MinNotional) {
		return domain.ErrInvalidAdminCreation
	}
	var account executionmodelv1.SimulatorAccount
	if err := tx.WithContext(ctx).Model(&executionmodelv1.SimulatorAccount{}).Where("account_id = ? AND enabled AND delete_at IS NULL", request.AccountID).
		Clauses(clause.Locking{Strength: "SHARE"}).Take(&account).Error; err != nil {
		return creationDependencyFailure(err)
	}
	var budget commonmodelv1.AccountBudget
	if err := tx.WithContext(ctx).Model(&commonmodelv1.AccountBudget{}).Where("account_id = ? AND enabled AND delete_at IS NULL", request.AccountID).
		Clauses(clause.Locking{Strength: "SHARE"}).Take(&budget).Error; err != nil {
		return creationDependencyFailure(err)
	}
	if budget.Asset != instrument.QuoteAsset {
		return domain.ErrInvalidAdminCreation
	}
	var portfolio portfoliomodelv1.PortfolioAccount
	if err := tx.WithContext(ctx).Model(&portfoliomodelv1.PortfolioAccount{}).Where("account_id = ? AND instrument_id = ? AND reconciled AND delete_at IS NULL", request.AccountID, request.InstrumentID).
		Clauses(clause.Locking{Strength: "SHARE"}).Take(&portfolio).Error; err != nil {
		return creationDependencyFailure(err)
	}
	if portfolio.BaseAsset != instrument.BaseAsset || portfolio.QuoteAsset != instrument.QuoteAsset {
		return domain.ErrInvalidAdminCreation
	}
	var feed marketmodelv1.MarketFeed
	if err := tx.WithContext(ctx).Model(&marketmodelv1.MarketFeed{}).
		Where("source = ? AND instrument_id = ? AND mode = ? AND delete_at IS NULL", request.Source, request.InstrumentID, request.Mode).
		Clauses(clause.Locking{Strength: "SHARE"}).Take(&feed).Error; err != nil {
		return creationDependencyFailure(err)
	}
	value, err := marketFeedValue(&feed)
	if err != nil {
		return domain.ErrInvalidAdminCreation
	}
	expected := instrument.BaseAsset + instrument.QuoteAsset
	if request.Source == domain.MarketSourceOKX {
		expected = instrument.BaseAsset + "-" + instrument.QuoteAsset
	}
	if value.Symbol != expected {
		return domain.ErrInvalidAdminCreation
	}
	return nil
}

func creationDependencyFailure(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ErrInvalidAdminCreation
	}
	return err
}
