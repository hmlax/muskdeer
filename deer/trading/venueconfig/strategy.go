package venueconfig

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	venuemigrate "github.com/hmlax/muskdeer/deer/migrate/venue"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	commonmodelv1 "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	marketmodelv1 "github.com/hmlax/muskdeer/deer/pb/market/model/v1"
	strategymodelv1 "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

const (
	venueParametersSchemaVersion = 2
	venueStrategyCodeVersion     = "venue-v1"
	venueStrategyCreatedBy       = "venue-policy-maintenance"
	venueStrategyPrefix          = "venue/"
)

// LoadBusiness reads one consistent startup snapshot from StrategyConfigVersion.
// Account budgets remain the sole source of fund bindings and financial limits.
func LoadBusiness(ctx context.Context, db *gorm.DB) (*configv1.BusinessRuntime, error) {
	if !policyContextValid(ctx) || db == nil {
		return nil, native.ErrInvalid
	}
	db = db.Session(&gorm.Session{NewDB: true})
	if err := venuemigrate.Check(ctx, db); err != nil {
		return nil, err
	}
	var result *configv1.BusinessRuntime
	err := db.Session(&gorm.Session{NewDB: true}).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var instances []*strategymodelv1.StrategyInstance
		if err := tx.Session(&gorm.Session{NewDB: true}).WithContext(ctx).Model(&strategymodelv1.StrategyInstance{}).Where("instance_key LIKE ? AND desired_running = ? AND delete_at IS NULL", venueStrategyPrefix+"%", true).Order("id").Find(&instances).Error; err != nil {
			return err
		}
		if len(instances) == 0 {
			return native.ErrInvalid
		}
		for _, instance := range instances {
			parameters, err := loadStrategyParameters(ctx, tx, instance)
			if err != nil {
				return err
			}
			if result == nil {
				result = parameters
				continue
			}
			if !sameRuntimeControls(result, parameters) {
				return native.ErrInvalid
			}
			result.Venue.Symbols = append(result.Venue.Symbols, parameters.Venue.Symbols...)
			if parameters.Venue.SyntheticTape != nil {
				result.Venue.SyntheticTape.Profiles = append(result.Venue.SyntheticTape.Profiles, parameters.Venue.SyntheticTape.Profiles...)
			}
		}
		return nil
	}, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	return result, err
}

// ImportStrategies appends immutable versions during explicit offline maintenance.
// It never funds accounts, submits orders or changes an existing startup snapshot.
func ImportStrategies(ctx context.Context, db *gorm.DB, input *commonv1.VenueStrategyImport) error {
	if !policyContextValid(ctx) || db == nil || len(input.GetStrategies()) == 0 {
		return native.ErrInvalid
	}
	db = db.Session(&gorm.Session{NewDB: true})
	if err := venuemigrate.Check(ctx, db); err != nil {
		return err
	}
	writes := make([]*commonv1.VenueStrategyInput, 0, len(input.Strategies))
	seen := make(map[string]bool, len(input.Strategies))
	for _, write := range input.Strategies {
		if write == nil || write.ExpectedRevision < 0 || !validStrategyParameters(write.Parameters, write.GetProfile().GetSymbol()) || !native.ValidProfile(write.Profile) || seen[write.Profile.Symbol] || write.BaseAsset == "" || write.QuoteAsset == "" || write.BaseAsset == write.QuoteAsset {
			return native.ErrInvalid
		}
		if _, err := native.Value(write.MinimumNotional, false, false); err != nil {
			return err
		}
		seen[write.Profile.Symbol] = true
		writes = append(writes, proto.CloneOf(write))
	}
	slices.SortFunc(writes, func(a, b *commonv1.VenueStrategyInput) int {
		return strings.Compare(native.ScopeKey(a.Profile.UserId, a.Profile.FundId), native.ScopeKey(b.Profile.UserId, b.Profile.FundId))
	})
	return db.Session(&gorm.Session{NewDB: true}).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := strategyMaintenanceAvailable(ctx, tx); err != nil {
			return err
		}
		for _, write := range writes {
			if err := writeStrategyParameters(ctx, tx, write); err != nil {
				return err
			}
		}
		_, err := LoadBusiness(ctx, tx)
		return err
	})
}

func loadStrategyParameters(ctx context.Context, db *gorm.DB, instance *strategymodelv1.StrategyInstance) (*configv1.BusinessRuntime, error) {
	var version strategymodelv1.StrategyConfigVersion
	if err := db.Session(&gorm.Session{NewDB: true}).WithContext(ctx).Model(&strategymodelv1.StrategyConfigVersion{}).Where("instance_id = ? AND version = ? AND delete_at IS NULL", instance.Id, instance.RequestedConfigVersion).Take(&version).Error; err != nil {
		return nil, err
	}
	var instrument commonmodelv1.Instrument
	if err := db.Session(&gorm.Session{NewDB: true}).WithContext(ctx).Model(&commonmodelv1.Instrument{}).Where("id = ? AND delete_at IS NULL", instance.InstrumentId).Take(&instrument).Error; err != nil {
		return nil, err
	}
	var budget commonmodelv1.TradingAccountPolicy
	if err := db.Session(&gorm.Session{NewDB: true}).WithContext(ctx).Model(&commonmodelv1.TradingAccountPolicy{}).Where("account_id = ? AND delete_at IS NULL", instance.AccountId).Take(&budget).Error; err != nil {
		return nil, err
	}
	profile, err := ProfileFromBudget(&budget)
	if err != nil || instrument.ExecutionVenue != native.Venue || instance.InstanceKey != venueStrategyPrefix+instrument.Symbol || profile.Symbol != instrument.Symbol || profile.MarketDataSource != instance.MarketDataSource || version.MarketDataSource != instance.MarketDataSource || version.ParametersSchemaVersion != venueParametersSchemaVersion || instance.Template != string(domain.StrategyTemplateMarketMaker) || version.GetTemplate() != instance.Template || version.CodeVersion != instance.CodeVersion || !validStrategyParameters(version.GetMakerParameters().GetVenueRuntime(), instrument.Symbol) {
		return nil, native.ErrInvalid
	}
	return proto.CloneOf(version.GetMakerParameters().GetVenueRuntime()), nil
}

func validStrategyParameters(parameters *configv1.BusinessRuntime, symbol string) bool {
	if parameters == nil || !parameters.Enabled || parameters.ExecutionVenue != native.Venue || native.ValidateParameters(parameters) != nil || parameters.PollInterval.CheckValid() != nil || parameters.PollInterval.AsDuration() < 10*time.Millisecond || parameters.PollInterval.AsDuration() > 5*time.Second || parameters.OperationTimeout.AsDuration() < 100*time.Millisecond || parameters.OperationTimeout.AsDuration() > 5*time.Second || len(parameters.Venue.Symbols) != 1 || parameters.Venue.Symbols[0] != symbol {
		return false
	}
	tape := parameters.Venue.SyntheticTape
	return tape == nil || len(tape.Profiles) == 1 && tape.Profiles[0].GetSymbol() == symbol
}

func sameRuntimeControls(a, b *configv1.BusinessRuntime) bool {
	left, right := proto.CloneOf(a), proto.CloneOf(b)
	for _, parameters := range []*configv1.BusinessRuntime{left, right} {
		parameters.Venue.Symbols = nil
		if parameters.Venue.SyntheticTape != nil {
			parameters.Venue.SyntheticTape.Profiles = nil
		}
	}
	return proto.Equal(left, right)
}

func writeStrategyParameters(ctx context.Context, tx *gorm.DB, input *commonv1.VenueStrategyInput) error {
	profile := input.Profile
	var budget commonmodelv1.TradingAccountPolicy
	if err := tx.Session(&gorm.Session{NewDB: true}).WithContext(ctx).Model(&commonmodelv1.TradingAccountPolicy{}).Where("account_id = ? AND delete_at IS NULL", native.ScopeKey(profile.UserId, profile.FundId)).Take(&budget).Error; err != nil {
		return err
	}
	stored, err := ProfileFromBudget(&budget)
	if err != nil || !proto.Equal(stored, profile) {
		return native.ErrInvalid
	}
	instrument := &commonmodelv1.Instrument{
		ExecutionVenue:  native.Venue,
		Symbol:          profile.Symbol,
		BaseAsset:       input.BaseAsset,
		QuoteAsset:      input.QuoteAsset,
		PriceTick:       profile.PriceTick,
		QuantityStep:    profile.QuantityStep,
		MinNotional:     input.MinimumNotional,
		Status:          domain.InstrumentStatusTrading,
		MetadataVersion: 1,
	}
	if err := tx.Session(&gorm.Session{NewDB: true}).WithContext(ctx).Model(&commonmodelv1.Instrument{}).Clauses(clause.OnConflict{DoNothing: true}).Create(instrument).Error; err != nil {
		return err
	}
	if err := tx.Session(&gorm.Session{NewDB: true}).WithContext(ctx).Model(&commonmodelv1.Instrument{}).Where("execution_venue = ? AND symbol = ? AND delete_at IS NULL", native.Venue, profile.Symbol).Take(instrument).Error; err != nil {
		return err
	}
	if instrument.BaseAsset != input.BaseAsset || instrument.QuoteAsset != input.QuoteAsset || instrument.Status != domain.InstrumentStatusTrading || !sameDecimal(instrument.PriceTick, profile.PriceTick) || !sameDecimal(instrument.QuantityStep, profile.QuantityStep) || !sameDecimal(instrument.MinNotional, input.MinimumNotional) {
		return native.ErrInvalid
	}
	key := venueStrategyPrefix + profile.Symbol
	var instance strategymodelv1.StrategyInstance
	err = tx.Session(&gorm.Session{NewDB: true}).WithContext(ctx).Model(&strategymodelv1.StrategyInstance{}).Clauses(clause.Locking{Strength: "UPDATE"}).Where("instance_key = ?", key).Take(&instance).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if input.ExpectedRevision != 0 {
			return native.ErrInvalid
		}
		instance = strategymodelv1.StrategyInstance{
			InstanceKey:            key,
			Template:               string(domain.StrategyTemplateMarketMaker),
			AccountId:              budget.AccountId,
			InstrumentId:           instrument.Id,
			CodeVersion:            venueStrategyCodeVersion,
			Revision:               1,
			State:                  string(domain.StrategyStateCreated),
			DesiredRunning:         true,
			RequestedConfigVersion: 1,
			MarketDataSource:       profile.MarketDataSource,
			MarketMode:             string(domain.MarketModeLive),
		}
		if err := tx.Session(&gorm.Session{NewDB: true}).WithContext(ctx).Model(&strategymodelv1.StrategyInstance{}).Create(&instance).Error; err != nil {
			return err
		}
		return createStrategyVersion(ctx, tx, &instance, input.Parameters)
	}
	if err != nil {
		return err
	}
	if instance.DeleteAt != nil || instance.AccountId != budget.AccountId || instance.InstrumentId != instrument.Id || instance.CodeVersion != venueStrategyCodeVersion || instance.Revision == math.MaxInt64 || instance.RequestedConfigVersion == math.MaxInt64 {
		return native.ErrInvalid
	}
	previous, err := loadStrategyParameters(ctx, tx, &instance)
	if err != nil {
		return err
	}
	if proto.Equal(previous, input.Parameters) {
		return nil
	}
	if instance.Revision != input.ExpectedRevision {
		return native.ErrInvalid
	}
	instance.Revision++
	instance.RequestedConfigVersion++
	if err := createStrategyVersion(ctx, tx, &instance, input.Parameters); err != nil {
		return err
	}
	return tx.Session(&gorm.Session{NewDB: true}).WithContext(ctx).Model(&strategymodelv1.StrategyInstance{}).Where("id = ?", instance.Id).Select("Revision", "RequestedConfigVersion").Updates(&instance).Error
}

func createStrategyVersion(ctx context.Context, tx *gorm.DB, instance *strategymodelv1.StrategyInstance, parameters *configv1.BusinessRuntime) error {
	return tx.Session(&gorm.Session{NewDB: true}).WithContext(ctx).Model(&strategymodelv1.StrategyConfigVersion{}).Create(&strategymodelv1.StrategyConfigVersion{
		InstanceId:              instance.Id,
		Version:                 instance.RequestedConfigVersion,
		MarketDataSource:        instance.MarketDataSource,
		CodeVersion:             instance.CodeVersion,
		CreatedBy:               venueStrategyCreatedBy,
		ParametersSchemaVersion: venueParametersSchemaVersion,
		Template:                proto.String(instance.Template),
		MakerParameters:         &commonv1.MakerParameters{VenueRuntime: proto.CloneOf(parameters)},
	}).Error
}

func strategyMaintenanceAvailable(ctx context.Context, tx *gorm.DB) error {
	for _, entity := range []any{&commonmodelv1.ExecutionShardLease{}, &marketmodelv1.MarketPublisherLease{}, &strategymodelv1.StrategyLease{}} {
		var active int64
		if err := tx.Session(&gorm.Session{NewDB: true}).WithContext(ctx).Model(entity).Where("delete_at IS NULL AND expires_at > floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint").Count(&active).Error; err != nil {
			return err
		}
		if active != 0 {
			return domain.ErrLeaseHeld
		}
	}
	return nil
}

func policyContextValid(ctx context.Context) bool {
	if ctx == nil || ctx.Err() != nil {
		return false
	}
	_, ok := ctx.Deadline()
	return ok
}

func sameDecimal(a, b string) bool {
	left, e1 := native.Value(a, false, false)
	right, e2 := native.Value(b, false, false)
	return e1 == nil && e2 == nil && left.Equal(right)
}
