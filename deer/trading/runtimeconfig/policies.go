package runtimeconfig

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	policymigrate "github.com/hmlax/muskdeer/deer/migrate/runtimepolicy"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	marketmodel "github.com/hmlax/muskdeer/deer/pb/market/model/v1"
	strategymodel "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

const (
	policyParametersSchemaVersion int64 = 1
	messagePolicyMissing                = "database runtime policy is required"
	messagePolicyConflict               = "database runtime policy revision differs"
)

// ErrPolicyMissing rejects startup without authoritative database parameters.
var ErrPolicyMissing = errors.New(messagePolicyMissing)

// ErrPolicyConflict rejects stale configuration revisions.
var ErrPolicyConflict = errors.New(messagePolicyConflict)

// LoadDatabasePolicy returns a private startup snapshot without a Nacos fallback.
// The supplied configuration contributes only service identity and infrastructure.
func LoadDatabasePolicy(ctx context.Context, db *gorm.DB, cfg *configv1.ServiceConfig) (*configv1.ServiceConfig, error) {
	if err := policyContext(ctx); err != nil {
		return nil, err
	}
	if db == nil || cfg == nil || !policyService(cfg.Name) {
		return nil, ErrInvalidConfig
	}
	if cfg.VenueConnection != nil {
		parameters, err := venueconfig.LoadBusiness(ctx, db)
		if err != nil {
			return nil, err
		}
		result := proto.CloneOf(cfg)
		result.Business = parameters
		if err := validatePolicyConfiguration(result); err != nil {
			return nil, err
		}
		return result, nil
	}
	if err := policymigrate.Check(ctx, db); err != nil {
		return nil, err
	}
	var row model.ServiceRuntimePolicy
	err := db.WithContext(ctx).Model(&model.ServiceRuntimePolicy{}).Where("service_name = ? AND delete_at IS NULL", cfg.Name).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPolicyMissing
	}
	if err != nil {
		return nil, err
	}
	if err := validatePolicyRow(&row); err != nil {
		return nil, err
	}
	if row.Parameters.Enabled && row.Parameters.ExecutionVenue == native.Venue {
		return nil, ErrInvalidConfig
	}
	result := proto.CloneOf(cfg)
	result.Business = proto.CloneOf(row.Parameters)
	if err := validatePolicyConfiguration(result); err != nil {
		return nil, err
	}
	return result, nil
}

// ImportDatabasePolicies commits an offline policy batch atomically. All trading
// nodes must be stopped first; active owners are rejected. Startup never calls it.
// Revision checks protect concurrent writers; this is not an online reconfiguration API.
func ImportDatabasePolicies(ctx context.Context, db *gorm.DB, input []*configv1.RuntimePolicyInput) ([]*model.ServiceRuntimePolicy, error) {
	if err := policyContext(ctx); err != nil {
		return nil, err
	}
	if db == nil || len(input) == 0 || len(input) > 4 {
		return nil, ErrInvalidConfig
	}
	writes := make([]*configv1.RuntimePolicyInput, 0, len(input))
	seen := make(map[string]bool, len(input))
	for _, write := range input {
		if write == nil || write.ExpectedRevision < 0 || ValidatePolicyParameters(write.ServiceName, write.Parameters) != nil || seen[write.ServiceName] {
			return nil, ErrInvalidConfig
		}
		seen[write.ServiceName] = true
		writes = append(writes, proto.CloneOf(write))
	}
	slices.SortFunc(writes, func(a, b *configv1.RuntimePolicyInput) int { return strings.Compare(a.ServiceName, b.ServiceName) })
	if err := policymigrate.Check(ctx, db); err != nil {
		return nil, err
	}
	var result []*model.ServiceRuntimePolicy
	err := db.WithContext(ctx).Model(&model.ServiceRuntimePolicy{}).Transaction(func(tx *gorm.DB) error {
		if err := policyMaintenanceAvailable(ctx, tx); err != nil {
			return err
		}
		for _, write := range writes {
			row, err := writePolicy(ctx, tx, write)
			if err != nil {
				return err
			}
			result = append(result, proto.CloneOf(row))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func writePolicy(ctx context.Context, tx *gorm.DB, write *configv1.RuntimePolicyInput) (*model.ServiceRuntimePolicy, error) {
	digest, err := policyDigest(write.Parameters)
	if err != nil {
		return nil, err
	}
	row := &model.ServiceRuntimePolicy{
		ServiceName:             write.ServiceName,
		Revision:                1,
		ParametersSchemaVersion: policyParametersSchemaVersion,
		Parameters:              proto.CloneOf(write.Parameters),
		ParametersDigest:        digest,
	}
	created := tx.WithContext(ctx).Model(&model.ServiceRuntimePolicy{}).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "service_name"}},
		DoNothing: true,
	}).Create(row)
	if created.Error != nil {
		return nil, created.Error
	}
	if created.RowsAffected == 1 {
		if write.ExpectedRevision != 0 {
			return nil, ErrPolicyConflict
		}
		return row, nil
	}
	var current model.ServiceRuntimePolicy
	if err := tx.WithContext(ctx).Model(&model.ServiceRuntimePolicy{}).Where("service_name = ?", row.ServiceName).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&current).Error; err != nil {
		return nil, err
	}
	if err := validatePolicyRow(&current); err != nil {
		return nil, err
	}
	if current.ParametersDigest == digest && proto.Equal(current.Parameters, row.Parameters) {
		return &current, nil
	}
	if current.Revision != write.ExpectedRevision || current.Revision == math.MaxInt64 {
		return nil, ErrPolicyConflict
	}
	current.Revision++
	current.Parameters = row.Parameters
	current.ParametersDigest = digest
	if err := tx.WithContext(ctx).Model(&model.ServiceRuntimePolicy{}).Where("id = ?", current.Id).Select("Revision", "Parameters", "ParametersDigest").Updates(&current).Error; err != nil {
		return nil, err
	}
	return &current, nil
}

func validatePolicyConfiguration(cfg *configv1.ServiceConfig) error {
	if cfg == nil || cfg.Business == nil || !policyService(cfg.Name) || cfg.Business.GetStrategy().GetManagementEnabled() || cfg.Business.GetStrategy().GetControlEnabled() {
		return ErrInvalidConfig
	}
	if err := ValidatePolicyParameters(cfg.Name, cfg.Business); err != nil {
		return err
	}
	return ValidateConfig(cfg)
}

func validatePolicyRow(row *model.ServiceRuntimePolicy) error {
	if row == nil || !policyService(row.ServiceName) || row.DeleteAt != nil || row.Revision <= 0 || row.ParametersSchemaVersion != policyParametersSchemaVersion || row.Parameters == nil {
		return ErrInvalidConfig
	}
	digest, err := policyDigest(row.Parameters)
	if err != nil {
		return err
	}
	if digest != row.ParametersDigest {
		return ErrInvalidConfig
	}
	return nil
}

func policyMaintenanceAvailable(ctx context.Context, tx *gorm.DB) error {
	for _, entity := range []any{&model.ExecutionShardLease{}, &marketmodel.MarketPublisherLease{}, &strategymodel.StrategyLease{}} {
		var active int64
		if err := tx.WithContext(ctx).Model(entity).Where("delete_at IS NULL AND expires_at > floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint").Count(&active).Error; err != nil {
			return err
		}
		if active != 0 {
			return domain.ErrLeaseHeld
		}
	}
	return nil
}

func policyService(name string) bool {
	switch name + ".grpc" {
	case rpcsecurity.ServiceMarket, rpcsecurity.ServiceStrategy, rpcsecurity.ServiceExecution, rpcsecurity.ServicePortfolio:
		return true
	default:
		return false
	}
}

func policyDigest(parameters *configv1.BusinessRuntime) (string, error) {
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(parameters)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(encoded)), nil
}

func policyContext(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidConfig
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return ErrInvalidConfig
	}
	return nil
}

// SeedDatabasePolicies is an explicit bootstrap operation. It creates missing
// records and preserves every existing policy, including disabled configurations.
func SeedDatabasePolicies(ctx context.Context, db *gorm.DB, input []*configv1.RuntimePolicyInput) error {
	if err := policyContext(ctx); err != nil {
		return err
	}
	if db == nil || len(input) == 0 || len(input) > 4 {
		return ErrInvalidConfig
	}
	if err := policymigrate.Check(ctx, db); err != nil {
		return err
	}
	var missing []*configv1.RuntimePolicyInput
	seen := make(map[string]bool, len(input))
	for _, policy := range input {
		if policy == nil || ValidatePolicyParameters(policy.ServiceName, policy.Parameters) != nil || seen[policy.ServiceName] {
			return ErrInvalidConfig
		}
		seen[policy.ServiceName] = true
		var current model.ServiceRuntimePolicy
		err := db.WithContext(ctx).Model(&model.ServiceRuntimePolicy{}).Where("service_name = ?", policy.ServiceName).Take(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			seed := proto.CloneOf(policy)
			seed.ExpectedRevision = 0
			missing = append(missing, seed)
			continue
		}
		if err != nil {
			return err
		}
		if err := validatePolicyRow(&current); err != nil {
			return err
		}
	}
	if len(missing) == 0 {
		return nil
	}
	_, err := ImportDatabasePolicies(ctx, db, missing)
	return err
}
