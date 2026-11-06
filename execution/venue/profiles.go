package venue

import (
	"context"
	"errors"
	"slices"
	"strings"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	commonmodelv1 "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

// ImportProfiles explicitly provisions database business policy while paused.
// Existing enablement and all financial observations/reservations are preserved;
// a new budget starts disabled and does not fund the native account.
func (s *Store) ImportProfiles(ctx context.Context, input *commonv1.VenueProfileImport) error {
	return s.configureProfiles(ctx, input.GetProfiles(), nil)
}

// InitializeBudget explicitly enables a reviewed database profile. It is never
// called by application startup and rejects any still-active affected owner.
func (s *Store) InitializeBudget(ctx context.Context, profile *commonv1.VenueProfilePolicy, enabled bool) error {
	return s.configureProfiles(ctx, []*commonv1.VenueProfilePolicy{profile}, &enabled)
}

func (s *Store) configureProfiles(ctx context.Context, input []*commonv1.VenueProfilePolicy, enabled *bool) error {
	if !validStoreContext(ctx) || len(input) == 0 || len(input) > native.MaxProfiles {
		return ErrInvalidSubmit
	}
	profiles := make([]*commonv1.VenueProfilePolicy, 0, len(input))
	funds, symbols := make(map[string]bool), make(map[string]bool)
	for _, profile := range input {
		row := budgetProfile(profile)
		if _, err := venueconfig.ProfileFromBudget(row); err != nil || funds[row.AccountId] || symbols[row.Symbol] {
			return ErrInvalidSubmit
		}
		funds[row.AccountId], symbols[row.Symbol] = true, true
		profiles = append(profiles, proto.CloneOf(profile))
	}
	slices.SortFunc(profiles, func(a, b *commonv1.VenueProfilePolicy) int {
		return strings.Compare(native.ScopeKey(a.UserId, a.FundId), native.ScopeKey(b.UserId, b.FundId))
	})
	return s.db.WithContext(ctx).Model(&commonmodelv1.TradingAccountPolicy{}).Transaction(func(tx *gorm.DB) error {
		for _, profile := range profiles {
			if err := configureProfile(ctx, tx, profile, enabled); err != nil {
				return err
			}
		}
		return nil
	})
}

func budgetProfile(p *commonv1.VenueProfilePolicy) *commonmodelv1.TradingAccountPolicy {
	return &commonmodelv1.TradingAccountPolicy{
		AccountId:            native.ScopeKey(p.GetUserId(), p.GetFundId()),
		UserId:               p.GetUserId(),
		FundId:               p.GetFundId(),
		Symbol:               p.GetSymbol(),
		MaximumNotional:      p.GetMaximumNotional(),
		MaximumOrderNotional: p.GetMaximumOrderNotional(),
		Leverage:             p.GetLeverage(),
		MarketDataSource:     p.GetMarketDataSource(),
		PriceTick:            p.GetPriceTick(),
		QuantityStep:         p.GetQuantityStep(),
		ObservedNotional:     "0",
		ReservedNotional:     "0",
		AvailableQuote:       "0",
		WorstFeeRate:         "0",
	}
}

func configureProfile(ctx context.Context, tx *gorm.DB, p *commonv1.VenueProfilePolicy, enabled *bool) error {
	input := budgetProfile(p)
	if err := tx.WithContext(ctx).Model(&commonmodelv1.ExecutionShardLease{}).Clauses(clause.OnConflict{DoNothing: true}).Create(&commonmodelv1.ExecutionShardLease{AccountId: input.AccountId}).Error; err != nil {
		return err
	}
	if err := lockProfileMaintenance(ctx, tx, input.AccountId, p); err != nil {
		return err
	}
	var current commonmodelv1.TradingAccountPolicy
	err := tx.WithContext(ctx).Model(&commonmodelv1.TradingAccountPolicy{}).Where("account_id = ?", input.AccountId).Clauses(clause.Locking{Strength: "UPDATE"}).First(&current).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if enabled != nil {
			input.Enabled = *enabled
		}
		return tx.WithContext(ctx).Model(&commonmodelv1.TradingAccountPolicy{}).Create(input).Error
	}
	if err != nil {
		return err
	}
	if current.DeleteAt != nil || current.UserId != p.UserId || current.FundId != p.FundId || current.Symbol != "" && current.Symbol != p.Symbol {
		return ErrInvalidSubmit
	}
	observed, e1 := domain.ParseLedgerDecimal(current.ObservedNotional, false, true)
	reserved, e2 := domain.ParseLedgerDecimal(current.ReservedNotional, false, true)
	maximum, e3 := domain.ParseLedgerDecimal(p.MaximumNotional, false, false)
	previous, e4 := domain.ParseLedgerDecimal(current.MaximumNotional, false, false)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || maximum.LessThan(previous) && maximum.LessThan(observed.Add(reserved)) {
		return ErrRiskLimit
	}
	input.Enabled = current.Enabled
	if enabled != nil {
		input.Enabled = *enabled
	}
	return tx.WithContext(ctx).Model(&commonmodelv1.TradingAccountPolicy{}).Where("id = ?", current.Id).
		Select("MaximumNotional", "MaximumOrderNotional", "Leverage", "Symbol", "MarketDataSource", "PriceTick", "QuantityStep", "Enabled").Updates(input).Error
}

func lockProfileMaintenance(ctx context.Context, tx *gorm.DB, account string, p *commonv1.VenueProfilePolicy) error {
	keys := []string{account, native.StrategyKey(p.UserId, p.FundId), native.PortfolioKey(p.UserId, p.FundId), native.MarketKey(p.Symbol), native.TradeKey(p.Symbol)}
	var owners []*commonmodelv1.ExecutionShardLease
	if err := tx.WithContext(ctx).Model(&commonmodelv1.ExecutionShardLease{}).Where("account_id IN ?", keys).Order("account_id").Clauses(clause.Locking{Strength: "UPDATE"}).Find(&owners).Error; err != nil {
		return err
	}
	var clock struct{ NowMS int64 }
	if err := tx.WithContext(ctx).Model(&commonmodelv1.ExecutionShardLease{}).Where("account_id = ?", account).Select("floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint AS now_ms").Take(&clock).Error; err != nil {
		return err
	}
	for _, owner := range owners {
		if owner.ExpiresAt > clock.NowMS {
			return domain.ErrLeaseHeld
		}
	}
	return nil
}
