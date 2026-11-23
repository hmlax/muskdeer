package venueconfig

import (
	"context"

	"gorm.io/gorm"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

// CurrentProfile reads live account risk parameters without mutating worker settings.
func CurrentProfile(ctx context.Context, db *gorm.DB, startup *common.VenueProfilePolicy) (*common.VenueProfilePolicy, error) {
	if ctx == nil || db == nil || startup == nil {
		return nil, native.ErrInvalid
	}
	var policy model.TradingAccountPolicy
	if err := db.Session(&gorm.Session{
		NewDB:   true,
		Context: ctx,
	}).Model(&model.TradingAccountPolicy{}).Where("account_id = ? AND delete_at IS NULL", native.ScopeKey(startup.UserId, startup.FundId)).Take(&policy).Error; err != nil {
		return nil, err
	}
	return ProfileWithPolicy(startup, &policy)
}

// ProfileWithPolicy permits only limits and leverage to change beneath a loaded instance.
func ProfileWithPolicy(startup *common.VenueProfilePolicy, policy *model.TradingAccountPolicy) (*common.VenueProfilePolicy, error) {
	current, err := ProfileFromBudget(policy)
	if err != nil || startup == nil {
		return nil, native.ErrInvalid
	}
	identity := current.UserId == startup.UserId && current.FundId == startup.FundId && current.Symbol == startup.Symbol
	rules := current.MarketDataSource == startup.MarketDataSource && current.PriceTick == startup.PriceTick && current.QuantityStep == startup.QuantityStep
	if !identity || !rules {
		return nil, native.ErrInvalid
	}
	current.StrategyInstanceId = startup.StrategyInstanceId
	return current, nil
}
