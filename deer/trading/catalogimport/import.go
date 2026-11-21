// Package catalogimport adds reviewed, stopped strategies without touching live instances.
package catalogimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/hmlax/muskdeer/deer/migrate/schemaformat"
	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

const importSubject = "catalog-import"

// Entry uses existing generated models/contracts, without another persistence schema.
type Entry struct {
	Instrument *model.Instrument             `json:"instrument"`
	Profile    *common.VenueProfilePolicy    `json:"profile"`
	Maker      *common.StrategyInstanceInput `json:"maker"`
	Wash       *common.StrategyInstanceInput `json:"wash"`
}

// Input contains only explicit, reviewed additions; existing records are never overwritten.
type Input struct {
	Entries []*Entry `json:"entries"`
}

// Result counts new records. A successful dry run rolls back every row.
type Result struct {
	Instruments int  `json:"instruments"`
	Accounts    int  `json:"accounts"`
	Instances   int  `json:"instances"`
	Applied     bool `json:"applied"`
}

var errPreview = errors.New("catalog preview rollback")

// Import atomically adds disabled accounts and stopped instances, preserving all
// existing identities, controls, parameters and financial observations. It never
// calls an SDK or performs DDL. Apply must be explicit after a successful preview.
func Import(ctx context.Context, db *gorm.DB, input *Input, apply bool) (Result, error) {
	result := Result{}
	if ctx == nil || db == nil || input == nil || len(input.Entries) == 0 {
		return result, instanceconfig.ErrInvalid
	}
	if err := schemaformat.Check(ctx, db); err != nil {
		return result, err
	}
	seen := make(map[string]bool)
	accounts := make(map[string]bool)
	for _, entry := range input.Entries {
		if !validEntry(entry) {
			return result, instanceconfig.ErrInvalid
		}
		account := native.ScopeKey(entry.Profile.UserId, entry.Profile.FundId)
		if seen[entry.Instrument.Symbol] || accounts[account] {
			return result, instanceconfig.ErrInvalid
		}
		seen[entry.Instrument.Symbol], accounts[account] = true, true
	}
	err := db.WithContext(ctx).Model(&model.Instrument{}).Transaction(func(tx *gorm.DB) error {
		for _, entry := range input.Entries {
			product, created, err := instrument(ctx, tx, entry.Instrument)
			if err != nil {
				return err
			}
			result.Instruments += created
			created, err = account(ctx, tx, entry.Profile)
			if err != nil {
				return err
			}
			result.Accounts += created
			for _, candidate := range []*common.StrategyInstanceInput{entry.Maker, entry.Wash} {
				write := proto.CloneOf(candidate)
				write.AccountId = native.ScopeKey(entry.Profile.UserId, entry.Profile.FundId)
				write.InstrumentId = product.Id
				created, err = instance(ctx, tx, write)
				if err != nil {
					return err
				}
				result.Instances += created
			}
		}
		if _, err := instanceconfig.LoadConfigured(ctx, tx); err != nil {
			return err
		}
		if !apply {
			return errPreview
		}
		return nil
	})
	if errors.Is(err, errPreview) {
		return result, nil
	}
	if err != nil {
		return Result{}, err
	}
	result.Applied = true
	return result, nil
}

func validEntry(entry *Entry) bool {
	if entry == nil || entry.Instrument == nil || entry.Profile == nil || entry.Maker == nil || entry.Wash == nil {
		return false
	}
	i, p := entry.Instrument, entry.Profile
	if i.Id != 0 || i.CreateAt != 0 || i.UpdateAt != 0 || i.DeleteAt != nil || i.ExecutionVenue != native.Venue || i.Status != domain.InstrumentStatusTrading || i.MetadataVersion <= 0 || i.BaseAsset == "" || i.QuoteAsset == "" || i.BaseAsset == i.QuoteAsset || i.Symbol != p.Symbol || !native.ValidProfile(p) || p.StrategyInstanceId != 0 {
		return false
	}
	if !sameDecimal(i.PriceTick, p.PriceTick) || !sameDecimal(i.QuantityStep, p.QuantityStep) {
		return false
	}
	if _, err := native.Value(i.MinNotional, false, false); err != nil {
		return false
	}
	for _, write := range []*common.StrategyInstanceInput{entry.Maker, entry.Wash} {
		if write.DesiredRunning || write.ExpectedRevision != 0 || write.InstrumentId != 0 || write.AccountId != "" || write.MarketDataSource != p.MarketDataSource || write.Parameters.GetExecution().GetLeverage() != p.Leverage {
			return false
		}
	}
	return entry.Maker.Parameters.GetMarketMaker() != nil && entry.Wash.Parameters.GetWashTrade().GetDevelopmentOnly()
}

func instrument(ctx context.Context, tx *gorm.DB, input *model.Instrument) (*model.Instrument, int, error) {
	var row model.Instrument
	err := tx.WithContext(ctx).Model(&model.Instrument{}).Where("execution_venue = ? AND symbol = ?", input.ExecutionVenue, input.Symbol).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		candidate := proto.CloneOf(input)
		err = tx.WithContext(ctx).Model(&model.Instrument{}).Create(candidate).Error
		return candidate, 1, err
	}
	if err != nil {
		return nil, 0, err
	}
	if row.DeleteAt != nil || row.BaseAsset != input.BaseAsset || row.QuoteAsset != input.QuoteAsset || row.Status != input.Status || row.MetadataVersion != input.MetadataVersion || !sameDecimal(row.PriceTick, input.PriceTick) || !sameDecimal(row.QuantityStep, input.QuantityStep) || !sameDecimal(row.MinNotional, input.MinNotional) {
		return nil, 0, fmt.Errorf("instrument %s: %w", input.Symbol, instanceconfig.ErrConflict)
	}
	return &row, 0, nil
}

func account(ctx context.Context, tx *gorm.DB, p *common.VenueProfilePolicy) (int, error) {
	key := native.ScopeKey(p.UserId, p.FundId)
	var row model.TradingAccountPolicy
	err := tx.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Where("account_id = ?", key).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 1, tx.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Create(&model.TradingAccountPolicy{
			AccountId:            key,
			UserId:               p.UserId,
			FundId:               p.FundId,
			Symbol:               p.Symbol,
			MarketDataSource:     p.MarketDataSource,
			PriceTick:            p.PriceTick,
			QuantityStep:         p.QuantityStep,
			MaximumNotional:      p.MaximumNotional,
			MaximumOrderNotional: p.MaximumOrderNotional,
			Leverage:             p.Leverage,
			Enabled:              false,
			ObservedNotional:     "0",
			ReservedNotional:     "0",
			AvailableQuote:       "0",
			WorstFeeRate:         "0",
		}).Error
	}
	if err != nil {
		return 0, err
	}
	if row.DeleteAt != nil || row.UserId != p.UserId || row.FundId != p.FundId || row.Symbol != p.Symbol || row.MarketDataSource != p.MarketDataSource || row.Leverage != p.Leverage || !sameDecimal(row.PriceTick, p.PriceTick) || !sameDecimal(row.QuantityStep, p.QuantityStep) || !sameDecimal(row.MaximumNotional, p.MaximumNotional) || !sameDecimal(row.MaximumOrderNotional, p.MaximumOrderNotional) {
		return 0, fmt.Errorf("account %s: %w", key, instanceconfig.ErrConflict)
	}
	return 0, nil
}

func instance(ctx context.Context, tx *gorm.DB, write *common.StrategyInstanceInput) (int, error) {
	var row strategy.StrategyInstance
	err := tx.WithContext(ctx).Model(&strategy.StrategyInstance{}).Where("instance_key = ?", write.InstanceKey).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row).Error
	if err == nil {
		if write.StrategyType != "" && write.StrategyType != row.Template || write.Implementation != "" && write.Implementation != row.Implementation {
			return 0, instanceconfig.ErrConflict
		}
		if row.DeleteAt != nil || row.TemplateId != write.TemplateId || row.TemplateRevision != write.TemplateRevision || row.AccountId != write.AccountId || row.InstrumentId != write.InstrumentId || row.MarketDataSource != write.MarketDataSource || !proto.Equal(row.Parameters, write.Parameters) {
			return 0, fmt.Errorf("instance %s: %w", write.InstanceKey, instanceconfig.ErrConflict)
		}
		return 0, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	id := sha256.Sum256([]byte(write.InstanceKey))
	_, err = instanceconfig.ManageConfigured(ctx, tx, instanceconfig.ManagementWrite{
		Operation: instanceconfig.ManagementCreate,
		Subject:   importSubject,
		RequestID: hex.EncodeToString(id[:]),
		Input:     write,
	})
	return 1, err
}

func sameDecimal(a, b string) bool {
	left, e1 := domain.ParseLedgerDecimal(a, false, true)
	right, e2 := domain.ParseLedgerDecimal(b, false, true)
	return e1 == nil && e2 == nil && left.Equal(right)
}
