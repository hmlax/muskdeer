package instanceconfig

import (
	"context"
	"errors"
	"math"
	"slices"
	"strings"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/hmlax/muskdeer/deer/migrate/schemaformat"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	commonmodel "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	model "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

const (
	currentCodeVersion         = "instance-v1"
	currentHistorySchema int64 = 3
	maintenanceCreator         = "instance-maintenance"
)

// Import applies a complete offline batch atomically. It neither enables funds
// nor resets tape quotas, changes another running snapshot, or sends SDK calls.
func Import(ctx context.Context, db *gorm.DB, input *common.StrategyInstanceImport, connections ...*configv1.VenueConnection) error {
	if validContext(ctx) != nil || db == nil || len(input.GetInstances()) == 0 {
		return ErrInvalid
	}
	if err := schemaformat.Check(ctx, db); err != nil {
		return err
	}
	writes := make([]*common.StrategyInstanceInput, 0, len(input.Instances))
	seen := make(map[string]bool)
	for _, write := range input.Instances {
		if write == nil || write.InstanceKey == "" || len(write.InstanceKey) > 64 || strings.ContainsAny(write.InstanceKey, "\t\r\n ") || write.TemplateId <= 0 || write.TemplateRevision <= 0 || write.AccountId == "" || write.InstrumentId <= 0 || write.ExpectedRevision < 0 || seen[write.InstanceKey] || write.Parameters == nil || domain.MarketSource(write.MarketDataSource).ValidateSelection() != nil {
			return ErrInvalid
		}
		seen[write.InstanceKey] = true
		writes = append(writes, proto.CloneOf(write))
	}
	slices.SortFunc(writes, func(a, b *common.StrategyInstanceInput) int { return strings.Compare(a.InstanceKey, b.InstanceKey) })
	return fresh(ctx, db).Transaction(func(tx *gorm.DB) error {
		var active int64
		if err := fresh(ctx, tx).Model(&commonmodel.ExecutionShardLease{}).Where("expires_at > floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint AND delete_at IS NULL").Count(&active).Error; err != nil {
			return err
		}
		if active != 0 {
			return domain.ErrLeaseHeld
		}
		templates, err := loadTemplates(ctx, tx)
		if err != nil {
			return err
		}
		for _, write := range writes {
			if err := bindTemplateInput(write, templates[write.TemplateId]); err != nil {
				return err
			}
			if err := writeInstance(ctx, tx, write); err != nil {
				return err
			}
		}
		_, err = Load(ctx, tx, connections...)
		return err
	})
}
func writeInstance(ctx context.Context, tx *gorm.DB, write *common.StrategyInstanceInput) error {
	var row model.StrategyInstance
	err := fresh(ctx, tx).Model(&model.StrategyInstance{}).Where("instance_key = ?", write.InstanceKey).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row).Error
	create := errors.Is(err, gorm.ErrRecordNotFound)
	if err != nil && !create {
		return err
	}
	if create {
		if write.ExpectedRevision != 0 {
			return ErrInvalid
		}
		row.InstanceKey = write.InstanceKey
		row.AccountId = write.AccountId
		row.InstrumentId = write.InstrumentId
		row.Template = write.StrategyType
		row.CodeVersion = currentCodeVersion
		row.Revision, row.RequestedConfigVersion = 1, 1
		row.MarketMode = string(domain.MarketModeLive)
		row.State = string(domain.StrategyStateCreated)
	} else {
		if row.DeleteAt != nil || row.AccountId != write.AccountId || row.InstrumentId != write.InstrumentId || row.Template != write.StrategyType {
			return ErrInvalid
		}
		if row.TemplateId == write.TemplateId && row.TemplateRevision == write.TemplateRevision && row.MarketDataSource == write.MarketDataSource && row.Implementation == write.Implementation && row.DesiredRunning == write.DesiredRunning && proto.Equal(row.Parameters, write.Parameters) {
			return nil
		}
		if row.Revision != write.ExpectedRevision || row.Revision == math.MaxInt64 || row.RequestedConfigVersion == math.MaxInt64 {
			return ErrInvalid
		}
		row.Revision++
		row.RequestedConfigVersion++
		if !proto.Equal(row.Parameters, write.Parameters) {
			row.TapePolicyDigest, row.TapeParametersDigest = "", ""
		}
	}
	row.TemplateId, row.TemplateRevision = write.TemplateId, write.TemplateRevision
	row.Parameters = proto.CloneOf(write.Parameters)
	row.Implementation = write.Implementation
	row.MarketDataSource = write.MarketDataSource
	row.DesiredRunning = write.DesiredRunning
	row.DesiredState = string(domain.StrategyStateStopped)
	if row.DesiredRunning {
		row.DesiredState = string(domain.StrategyStateRunning)
	}
	if create {
		if err := fresh(ctx, tx).Model(&model.StrategyInstance{}).Create(&row).Error; err != nil {
			return err
		}
	} else {
		if err := fresh(ctx, tx).Model(&model.StrategyInstance{}).Where("id = ?", row.Id).Select("TemplateId", "TemplateRevision", "Parameters", "Implementation", "MarketDataSource", "DesiredRunning", "DesiredState", "Revision", "RequestedConfigVersion", "TapePolicyDigest", "TapeParametersDigest").Updates(&row).Error; err != nil {
			return err
		}
	}
	kind := row.Template
	return fresh(ctx, tx).Model(&model.StrategyConfigVersion{}).Create(&model.StrategyConfigVersion{
		InstanceId:              row.Id,
		TemplateId:              &row.TemplateId,
		TemplateRevision:        &row.TemplateRevision,
		Version:                 row.RequestedConfigVersion,
		MarketDataSource:        row.MarketDataSource,
		CodeVersion:             row.CodeVersion,
		CreatedBy:               maintenanceCreator,
		ParametersSchemaVersion: currentHistorySchema,
		Template:                &kind,
		Parameters:              proto.CloneOf(row.Parameters),
	}).Error
}

func bindTemplateInput(write *common.StrategyInstanceInput, template *model.StrategyTemplate) error {
	if template == nil || write.TemplateRevision != template.Revision || write.DesiredRunning && !template.Enabled {
		return ErrInvalid
	}
	if write.StrategyType != "" && write.StrategyType != template.StrategyType || write.Implementation != "" && write.Implementation != template.Implementation {
		return ErrInvalid
	}
	write.StrategyType, write.Implementation = template.StrategyType, template.Implementation
	return Validate(domain.StrategyTemplate(write.StrategyType), write.Implementation, write.Parameters)
}
