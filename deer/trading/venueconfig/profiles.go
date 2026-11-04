package venueconfig

import (
	"context"
	"time"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	commonmodelv1 "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

// RuntimeConfig contains a private startup snapshot of database business policy.
// Nacos still supplies transport and worker timing; loading never writes data.
type RuntimeConfig struct {
	PollInterval     time.Duration
	OperationTimeout time.Duration
	Himalaya         *commonv1.HimalayaPolicy
}

// LoadRuntime borrows the existing pool and rejects missing or invalid profiles.
// Disabled budgets remain visible for observation and controlled cancellation.
func LoadRuntime(ctx context.Context, db *gorm.DB, cfg *configv1.BusinessRuntime) (*RuntimeConfig, error) {
	if ctx == nil || db == nil || cfg.GetHimalaya() == nil || cfg.GetOperationTimeout().CheckValid() != nil || cfg.OperationTimeout.AsDuration() <= 0 {
		return nil, native.ErrInvalid
	}
	op, cancel := context.WithTimeout(ctx, cfg.OperationTimeout.AsDuration())
	defer cancel()
	var rows []*commonmodelv1.TradingAccountPolicy
	if err := db.WithContext(op).Model(&commonmodelv1.TradingAccountPolicy{}).Where("delete_at IS NULL").Order("id").Limit(9).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 || len(rows) > 8 {
		return nil, native.ErrInvalid
	}
	h := cfg.Himalaya
	policy := &commonv1.HimalayaPolicy{
		Target:           h.Target,
		TokenEnv:         h.TokenEnv,
		QuantityFraction: h.QuantityFraction,
		Depth:            h.Depth,
		LeaseTtl:         proto.CloneOf(h.LeaseTtl),
		SyntheticTape:    proto.CloneOf(h.SyntheticTape),
	}
	funds, symbols := make(map[string]bool), make(map[string]bool)
	for _, row := range rows {
		profile, err := ProfileFromBudget(row)
		if err != nil || funds[row.AccountId] || symbols[profile.GetSymbol()] {
			return nil, native.ErrInvalid
		}
		funds[row.AccountId], symbols[profile.Symbol] = true, true
		policy.Profiles = append(policy.Profiles, profile)
	}
	if err := native.ValidateSyntheticConfig(policy); err != nil {
		return nil, err
	}
	return &RuntimeConfig{
		PollInterval:     cfg.PollInterval.AsDuration(),
		OperationTimeout: cfg.OperationTimeout.AsDuration(),
		Himalaya:         policy,
	}, nil
}

// ProfileFromBudget maps the generated model directly to immutable business
// fields, normalizing database numeric padding without a JSON conversion bridge.
func ProfileFromBudget(row *commonmodelv1.TradingAccountPolicy) (*commonv1.HimalayaProfilePolicy, error) {
	if row == nil || row.DeleteAt != nil || row.AccountId != native.ScopeKey(row.UserId, row.FundId) {
		return nil, native.ErrInvalid
	}
	maximum, e1 := domain.ParseLedgerDecimal(row.MaximumNotional, false, false)
	order, e2 := domain.ParseLedgerDecimal(row.MaximumOrderNotional, false, false)
	tick, e3 := domain.ParseLedgerDecimal(row.PriceTick, false, false)
	step, e4 := domain.ParseLedgerDecimal(row.QuantityStep, false, false)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		return nil, native.ErrInvalid
	}
	profile := &commonv1.HimalayaProfilePolicy{
		UserId:               row.UserId,
		FundId:               row.FundId,
		Symbol:               row.Symbol,
		MaximumNotional:      maximum.String(),
		MaximumOrderNotional: order.String(),
		Leverage:             row.Leverage,
		MarketDataSource:     row.MarketDataSource,
		PriceTick:            tick.String(),
		QuantityStep:         step.String(),
	}
	if !native.ValidProfile(profile) {
		return nil, native.ErrInvalid
	}
	return profile, nil
}
