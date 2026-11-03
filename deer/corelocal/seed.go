// Package corelocal explicitly prepares unfunded local core runtime resources.
package corelocal

import (
	"context"
	"errors"
	"strconv"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/market/marketquality"
)

const (
	// AccountID identifies only the disabled, unfunded local simulator account.
	AccountID  = "muskdeer-local-unfunded"
	subjectID  = "muskdeer-local-simulator"
	symbol     = "BTCUSDT"
	baseAsset  = "BTC"
	quoteAsset = "USDT"
)

// ErrResources rejects incompatible existing local identities without changing them.
var ErrResources = errors.New("local core resources conflict with existing identities")

// Resources contains persistent resource identities, never credentials or money.
type Resources struct {
	AccountID     string `json:"account_id"`
	InstrumentID  string `json:"instrument_id"`
	BinanceFeedID string `json:"binance_feed_id"`
	OKXFeedID     string `json:"okx_feed_id"`
}

// Seed creates only immutable replay bindings and a disabled, unfunded account.
// Replays never update budgets, existing resources, strategy targets or balances.
func Seed(ctx context.Context, db *gorm.DB) (Resources, error) {
	if ctx == nil || db == nil {
		return Resources{}, ErrResources
	}
	var result Resources
	err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Transaction(func(tx *gorm.DB) error {
		instrument := &modelv1.Instrument{
			ExecutionVenue:  domain.ExecutionVenueSimulator,
			Symbol:          symbol,
			BaseAsset:       baseAsset,
			QuoteAsset:      quoteAsset,
			PriceTick:       "0.01",
			QuantityStep:    "0.001",
			MinNotional:     "1",
			Status:          domain.InstrumentStatusTrading,
			MetadataVersion: 1,
		}
		if err := tx.WithContext(ctx).Model(&modelv1.Instrument{}).Clauses(clause.OnConflict{DoNothing: true}).Create(instrument).Error; err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Model(&modelv1.Instrument{}).Where("execution_venue = ? AND symbol = ? AND delete_at IS NULL", domain.ExecutionVenueSimulator, symbol).First(instrument).Error; err != nil {
			return err
		}
		if instrument.BaseAsset != baseAsset || instrument.QuoteAsset != quoteAsset || instrument.MetadataVersion != 1 {
			return ErrResources
		}
		if err := seedAccount(ctx, tx); err != nil {
			return err
		}
		result.AccountID, result.InstrumentID = AccountID, strconv.FormatInt(instrument.Id, 10)
		for _, source := range []domain.MarketSource{domain.MarketSourceBinance, domain.MarketSourceOKX} {
			external := symbol
			if source == domain.MarketSourceOKX {
				external = baseAsset + "-" + quoteAsset
			}
			feedID, err := seedFeed(ctx, tx, instrument.Id, source, external)
			if err != nil {
				return err
			}
			if source == domain.MarketSourceBinance {
				result.BinanceFeedID = strconv.FormatInt(feedID, 10)
			} else {
				result.OKXFeedID = strconv.FormatInt(feedID, 10)
			}
		}
		return nil
	})
	if err != nil {
		return Resources{}, err
	}
	return result, nil
}

func seedFeed(ctx context.Context, tx *gorm.DB, instrumentID int64, source domain.MarketSource, external string) (int64, error) {
	policy := marketquality.DefaultPolicy()
	storedPolicy := &commonv1.MarketPolicy{
		MaximumAgeMs:     policy.MaximumAge.Milliseconds(),
		MaximumSkewMs:    policy.MaximumSkew.Milliseconds(),
		MaximumSpreadBps: policy.MaximumSpreadBP.String(),
		MaximumJumpBps:   policy.MaximumJumpBP.String(),
		RecoverySamples:  int32(policy.RecoverySamples),
		RecoveryWindowMs: policy.RecoveryWindow.Milliseconds(),
	}
	row := &modelv1.MarketFeed{
		Source:         string(source),
		InstrumentId:   instrumentID,
		ExternalSymbol: external,
		Mode:           string(domain.MarketModeReplay),
		SourceVersion:  domain.MarketSourceVersion,
		Policy:         storedPolicy,
	}
	if err := tx.WithContext(ctx).Model(&modelv1.MarketFeed{}).Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error; err != nil {
		return 0, err
	}
	var saved modelv1.MarketFeed
	if err := tx.WithContext(ctx).Model(&modelv1.MarketFeed{}).
		Where("source = ? AND instrument_id = ? AND mode = ? AND delete_at IS NULL", source, instrumentID, domain.MarketModeReplay).First(&saved).Error; err != nil {
		return 0, err
	}
	if saved.ExternalSymbol != external || saved.SourceVersion != domain.MarketSourceVersion || !proto.Equal(saved.Policy, storedPolicy) {
		return 0, ErrResources
	}
	return saved.Id, nil
}

func seedAccount(ctx context.Context, tx *gorm.DB) error {
	account := &modelv1.SimulatorAccount{
		AccountId:    AccountID,
		SubjectId:    subjectID,
		Enabled:      false,
		QuoteFeeRate: "0",
		Revision:     1,
	}
	if err := tx.WithContext(ctx).Model(&modelv1.SimulatorAccount{}).Clauses(clause.OnConflict{DoNothing: true}).Create(account).Error; err != nil {
		return err
	}
	if err := tx.WithContext(ctx).Model(&modelv1.SimulatorAccount{}).Where("account_id = ? AND delete_at IS NULL", AccountID).First(account).Error; err != nil {
		return err
	}
	if account.SubjectId != subjectID || account.Enabled {
		return ErrResources
	}
	budget := &modelv1.AccountBudget{
		AccountId:        AccountID,
		Asset:            quoteAsset,
		LimitNotional:    "1",
		ReservedNotional: "0",
		Enabled:          false,
		Revision:         1,
	}
	if err := tx.WithContext(ctx).Model(&modelv1.AccountBudget{}).Clauses(clause.OnConflict{DoNothing: true}).Create(budget).Error; err != nil {
		return err
	}
	if err := tx.WithContext(ctx).Model(&modelv1.AccountBudget{}).Where("account_id = ? AND delete_at IS NULL", AccountID).First(budget).Error; err != nil {
		return err
	}
	if budget.Asset != quoteAsset || budget.Enabled {
		return ErrResources
	}
	return nil
}
