package instanceconfig

import (
	"context"
	"errors"
	"math"
	"strings"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	config "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	commonmodel "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	model "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

// Management operation identities are preserved in immutable audit records.
const (
	ManagementCreate = "create"
	ManagementUpdate = "update"
	ManagementStart  = "start"
	ManagementStop   = "stop"
)

// ErrConflict requires a refresh instead of retrying a stale management write.
var ErrConflict = errors.New("strategy instance changed; refresh before retrying")

// ManagementWrite is an authenticated business command, never a persistence model.
type ManagementWrite struct {
	Operation              string
	Subject                string
	RequestID              string
	ID                     int64
	ExpectedRevision       int64
	ExpectedControlVersion int64
	// nil means platform scope; merchants must supply a nonempty, verified scope.
	Accounts []string
	Input    *common.StrategyInstanceInput
}

// Manage atomically applies one versioned command and appends its complete
// historical parameters. It never reloads workers or modifies financial policy.
func Manage(ctx context.Context, db *gorm.DB, command ManagementWrite, connection *config.VenueConnection) (*model.StrategyInstance, error) {
	return manage(ctx, db, command, connection, false)
}

// ManageConfigured changes configuration/control only; it never reads or
// rewrites historical WashTrade batches or receipts.
func ManageConfigured(ctx context.Context, db *gorm.DB, command ManagementWrite) (*model.StrategyInstance, error) {
	return manage(ctx, db, command, nil, true)
}

func manage(ctx context.Context, db *gorm.DB, command ManagementWrite, connection *config.VenueConnection, direct bool) (*model.StrategyInstance, error) {
	if validContext(ctx) != nil || db == nil || len(command.Subject) == 0 || len(command.Subject) > 64 || len(command.RequestID) < 16 || len(command.RequestID) > 128 {
		return nil, ErrInvalid
	}
	var result *model.StrategyInstance
	err := fresh(ctx, db).Transaction(func(tx *gorm.DB) error {
		row := &model.StrategyInstance{}
		create := command.Operation == ManagementCreate
		if !create {
			query := fresh(ctx, tx).Model(&model.StrategyInstance{}).Where("id = ? AND delete_at IS NULL", command.ID)
			if command.Accounts != nil {
				query = query.Where("account_id IN ?", command.Accounts)
			}
			if err := query.Clauses(clause.Locking{Strength: "UPDATE"}).Take(row).Error; err != nil {
				return err
			}
			if row.Revision != command.ExpectedRevision || row.RequestedConfigVersion != command.ExpectedControlVersion || row.RequestedConfigVersion == math.MaxInt64 {
				return ErrConflict
			}
		}
		if err := applyManagement(ctx, tx, row, command, connection, direct); err != nil {
			return err
		}
		row.DesiredState = string(domain.StrategyStateStopped)
		if row.DesiredRunning {
			row.DesiredState = string(domain.StrategyStateRunning)
		}
		var template model.StrategyTemplate
		var instrument commonmodel.Instrument
		var budget commonmodel.TradingAccountPolicy
		if err := fresh(ctx, tx).Model(&model.StrategyTemplate{}).Where("id = ? AND delete_at IS NULL", row.TemplateId).Take(&template).Error; err != nil {
			return err
		}
		if err := fresh(ctx, tx).Model(&commonmodel.Instrument{}).Where("id = ? AND delete_at IS NULL", row.InstrumentId).Take(&instrument).Error; err != nil {
			return err
		}
		if err := fresh(ctx, tx).Model(&commonmodel.TradingAccountPolicy{}).Where("account_id = ? AND delete_at IS NULL", row.AccountId).Take(&budget).Error; err != nil {
			return err
		}
		// fromRow validates full binding; a new row gets its real identity on insert.
		if create {
			if err := fresh(ctx, tx).Model(&model.StrategyInstance{}).Create(row).Error; err != nil {
				return err
			}
		}
		input := row
		if direct {
			input = configuredRow(row)
		}
		instance, err := fromRow(input, &template, &instrument, &budget)
		if err != nil {
			return err
		}
		if !direct && instance.Type == domain.StrategyTemplateWashTrade {
			if err := resumeTape(ctx, tx, instance, connection); err != nil {
				return err
			}
		}
		if !create {
			if err := fresh(ctx, tx).Model(&model.StrategyInstance{}).Where("id = ?", row.Id).
				Select("Parameters", "Revision", "RequestedConfigVersion", "DesiredRunning", "DesiredState", "TapePolicyDigest", "TapeParametersDigest").Updates(row).Error; err != nil {
				return err
			}
		}
		history := proto.CloneOf(row.Parameters)
		history.ManagementChange = &common.StrategyManagementChange{
			Operation:              command.Operation,
			DesiredRunning:         proto.Bool(row.DesiredRunning),
			RequestId:              command.RequestID,
			ExpectedRevision:       command.ExpectedRevision,
			ExpectedControlVersion: command.ExpectedControlVersion,
		}
		if err := fresh(ctx, tx).Model(&model.StrategyConfigVersion{}).Create(&model.StrategyConfigVersion{
			InstanceId:              row.Id,
			TemplateId:              proto.Int64(row.TemplateId),
			TemplateRevision:        proto.Int64(row.TemplateRevision),
			Version:                 row.RequestedConfigVersion,
			MarketDataSource:        row.MarketDataSource,
			CodeVersion:             row.CodeVersion,
			CreatedBy:               command.Subject,
			ParametersSchemaVersion: currentHistorySchema,
			Template:                proto.String(row.Template),
			Parameters:              history,
		}).Error; err != nil {
			return err
		}
		result = proto.CloneOf(row)
		return nil
	})
	return result, err
}

func applyManagement(ctx context.Context, tx *gorm.DB, row *model.StrategyInstance, command ManagementWrite, connection *config.VenueConnection, direct bool) error {
	switch command.Operation {
	case ManagementCreate:
		if err := createManagement(ctx, tx, row, command); err != nil {
			return err
		}
	case ManagementUpdate:
		if row.DesiredRunning || row.Revision == math.MaxInt64 || command.Input == nil {
			return ErrConflict
		}
		in := &common.StrategyInstanceInput{
			TemplateId:       row.TemplateId,
			TemplateRevision: row.TemplateRevision,
			MarketDataSource: row.MarketDataSource,
			Implementation:   row.Implementation,
			Parameters:       proto.CloneOf(command.Input.Parameters),
		}
		if err := Validate(domain.StrategyTemplate(row.Template), row.Implementation, in.Parameters); err != nil {
			return err
		}
		if !direct {
			if err := preserveUnlimitedTape(ctx, tx, tapeUpgrade{
				row:        row,
				write:      in,
				connection: connection,
			}); err != nil {
				return err
			}
		}
		row.Parameters = in.Parameters
		row.Revision++
		row.RequestedConfigVersion++
	case ManagementStart, ManagementStop:
		row.DesiredRunning = command.Operation == ManagementStart
		row.RequestedConfigVersion++
	default:
		return ErrInvalid
	}
	return nil
}

func createManagement(ctx context.Context, tx *gorm.DB, row *model.StrategyInstance, command ManagementWrite) error {
	in := command.Input
	if in == nil || in.InstanceKey == "" || len(in.InstanceKey) > 64 || strings.ContainsAny(in.InstanceKey, "\t\r\n ") || in.AccountId == "" || in.InstrumentId <= 0 || in.DesiredRunning {
		return ErrInvalid
	}
	if command.Accounts != nil {
		allowed := false
		for _, account := range command.Accounts {
			allowed = allowed || account == in.AccountId
		}
		if !allowed {
			return gorm.ErrRecordNotFound
		}
	}
	row.InstanceKey, row.AccountId, row.InstrumentId = in.InstanceKey, in.AccountId, in.InstrumentId
	row.TemplateId, row.TemplateRevision = in.TemplateId, in.TemplateRevision
	row.MarketDataSource = in.MarketDataSource
	row.Revision, row.RequestedConfigVersion = 1, 1
	row.CodeVersion = currentCodeVersion
	row.MarketMode = string(domain.MarketModeLive)
	row.State = string(domain.StrategyStateCreated)
	var template model.StrategyTemplate
	if err := fresh(ctx, tx).Model(&model.StrategyTemplate{}).Where("id = ? AND delete_at IS NULL", in.TemplateId).Take(&template).Error; err != nil {
		return err
	}
	if err := bindTemplateInput(in, &template); err != nil {
		return err
	}
	row.Template, row.Implementation = in.StrategyType, in.Implementation
	row.Parameters = proto.CloneOf(in.Parameters)

	return nil
}
