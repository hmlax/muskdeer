package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func lockCreationDependencies(ctx context.Context, tx *gorm.DB, request domain.AdminCreateStrategy) error {
	// Match core's instrument-before-account order. Creation holds no existing
	// strategy lock, and all dependency reads remain shared until commit.
	var instrument modelv1.Instrument
	if err := tx.WithContext(ctx).Model(&modelv1.Instrument{}).Where("id = ? AND delete_at IS NULL", request.InstrumentID).
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
	var account modelv1.SimulatorAccount
	if err := tx.WithContext(ctx).Model(&modelv1.SimulatorAccount{}).Where("account_id = ? AND enabled AND delete_at IS NULL", request.AccountID).
		Clauses(clause.Locking{Strength: "SHARE"}).Take(&account).Error; err != nil {
		return creationDependencyFailure(err)
	}
	var budget modelv1.AccountBudget
	if err := tx.WithContext(ctx).Model(&modelv1.AccountBudget{}).Where("account_id = ? AND enabled AND delete_at IS NULL", request.AccountID).
		Clauses(clause.Locking{Strength: "SHARE"}).Take(&budget).Error; err != nil {
		return creationDependencyFailure(err)
	}
	if budget.Asset != instrument.QuoteAsset {
		return domain.ErrInvalidAdminCreation
	}
	var portfolio modelv1.PortfolioAccount
	if err := tx.WithContext(ctx).Model(&modelv1.PortfolioAccount{}).Where("account_id = ? AND instrument_id = ? AND reconciled AND delete_at IS NULL", request.AccountID, request.InstrumentID).
		Clauses(clause.Locking{Strength: "SHARE"}).Take(&portfolio).Error; err != nil {
		return creationDependencyFailure(err)
	}
	if portfolio.BaseAsset != instrument.BaseAsset || portfolio.QuoteAsset != instrument.QuoteAsset {
		return domain.ErrInvalidAdminCreation
	}
	var feed modelv1.MarketFeed
	if err := tx.WithContext(ctx).Model(&modelv1.MarketFeed{}).
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
