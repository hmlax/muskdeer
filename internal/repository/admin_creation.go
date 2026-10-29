package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategyconfig"
)

type adminStrategyCreator struct{ db *gorm.DB }

// NewAdminStrategyCreator borrows storage; only explicit Create writes metadata.
func NewAdminStrategyCreator(db *gorm.DB) (domain.AdminStrategyCreator, error) {
	if db == nil {
		return nil, domain.ErrInvalidAdminCreation
	}
	return &adminStrategyCreator{db: db}, nil
}

func (s *adminStrategyCreator) Create(ctx context.Context, token, csrf string, request domain.AdminCreateStrategy) (domain.AdminCreationReceipt, error) {
	if err := coreContext(ctx); err != nil {
		return domain.AdminCreationReceipt{}, err
	}
	request, err := request.Normalize()
	if err != nil {
		return domain.AdminCreationReceipt{}, err
	}
	parameters, err := strategyconfig.Encode(request.Parameters)
	if err != nil {
		return domain.AdminCreationReceipt{}, domain.ErrInvalidAdminCreation
	}
	// Own canonical typed values before fingerprinting or waiting for any lock.
	request.Parameters, err = strategyconfig.Decode(domain.ParametersSchemaVersion, parameters)
	if err != nil {
		return domain.AdminCreationReceipt{}, domain.ErrInvalidAdminCreation
	}
	fingerprint, err := adminCreationFingerprint(request, parameters)
	if err != nil {
		return domain.AdminCreationReceipt{}, adminCreationFailure(err)
	}
	var result domain.AdminCreationReceipt
	err = s.db.WithContext(ctx).Model(&modelv1.AdminStrategyCreation{}).Transaction(func(tx *gorm.DB) error {
		user, session, err := readAdminControlAuthority(ctx, tx, token, csrf)
		if err != nil {
			return err
		}
		var existing modelv1.AdminStrategyCreation
		err = tx.WithContext(ctx).Model(&modelv1.AdminStrategyCreation{}).
			Where("actor_user_id = ? AND idempotency_key = ?", user.Id, request.Key).Take(&existing).Error
		if err == nil {
			if existing.RequestFingerprint != fingerprint {
				return domain.ErrAdminConflict
			}
			result = adminCreationReceipt(&existing)
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := lockCreationDependencies(ctx, tx, request); err != nil {
			return err
		}
		instance := &modelv1.StrategyInstance{
			InstanceKey:            request.InstanceKey,
			Template:               string(request.Parameters.Template),
			AccountId:              request.AccountID,
			InstrumentId:           request.InstrumentID,
			CodeVersion:            request.CodeVersion,
			Revision:               1,
			State:                  string(domain.StrategyStateCreated),
			DesiredState:           string(domain.StrategyStateStopped),
			RequestedConfigVersion: 1,
			MarketDataSource:       string(request.Source),
			MarketMode:             string(request.Mode),
		}
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Create(instance).Error; err != nil {
			return err
		}
		version := &modelv1.StrategyConfigVersion{
			InstanceId:              instance.Id,
			Version:                 1,
			MarketDataSource:        string(request.Source),
			CodeVersion:             request.CodeVersion,
			CreatedBy:               user.Username,
			ParametersSchemaVersion: domain.ParametersSchemaVersion,
			Template:                proto.String(instance.Template),
			MakerParameters:         parameters.GetMarketMaker(),
			GridParameters:          parameters.GetGrid(),
		}
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Create(version).Error; err != nil {
			return err
		}
		limit := strategyParameterBudget(request.Parameters)
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Create(&modelv1.StrategyBudget{
			StrategyInstanceId: instance.Id,
			AccountId:          request.AccountID,
			LimitNotional:      limit.String(),
			ReservedNotional:   "0",
			Enabled:            true,
			Revision:           1,
		}).Error; err != nil {
			return err
		}
		audit := &modelv1.AdminAudit{
			Actor:        user.Username,
			ActorRole:    user.Role,
			Action:       domain.AdminStrategyCreate,
			Resource:     domain.AdminAuditResourceStrategy + ":" + strconv.FormatInt(instance.Id, 10),
			Reason:       request.Reason,
			AfterVersion: proto.Int64(1),
		}
		if err := tx.WithContext(ctx).Model(&modelv1.AdminAudit{}).Create(audit).Error; err != nil {
			return err
		}
		receipt := &modelv1.AdminStrategyCreation{
			ActorUserId:        user.Id,
			ActorSessionId:     session.Id,
			StrategyInstanceId: instance.Id,
			AuditId:            audit.Id,
			IdempotencyKey:     request.Key,
			RequestFingerprint: fingerprint,
			ConfigVersion:      1,
			Revision:           1,
			MarketDataSource:   string(request.Source),
			Reason:             request.Reason,
		}
		if err := tx.WithContext(ctx).Model(&modelv1.AdminStrategyCreation{}).Create(receipt).Error; err != nil {
			return err
		}
		result = adminCreationReceipt(receipt)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return domain.AdminCreationReceipt{}, adminCreationFailure(err)
	}
	return result, nil
}

func adminCreationReceipt(row *modelv1.AdminStrategyCreation) domain.AdminCreationReceipt {
	return domain.AdminCreationReceipt{
		ID:            row.Id,
		ActorUserID:   row.ActorUserId,
		StrategyID:    row.StrategyInstanceId,
		ConfigVersion: row.ConfigVersion,
		Revision:      row.Revision,
		Source:        domain.MarketSource(row.MarketDataSource),
		Reason:        row.Reason,
		CreatedAt:     time.UnixMilli(row.CreateAt).UTC(),
	}
}

func adminCreationFingerprint(request domain.AdminCreateStrategy, parameters *commonv1.StrategyParameters) (string, error) {
	encoded, err := (proto.MarshalOptions{Deterministic: true}).Marshal(parameters)
	if err != nil {
		return "", err
	}
	data := binary.BigEndian.AppendUint64(nil, uint64(request.InstrumentID))
	for _, field := range []string{domain.AdminStrategyCreate, request.InstanceKey, request.AccountID, request.CodeVersion, string(request.Source), string(request.Mode), request.Reason, string(encoded)} {
		data = binary.BigEndian.AppendUint32(data, uint32(len(field)))
		data = append(data, field...)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func adminCreationFailure(err error) error {
	if errors.Is(err, domain.ErrInvalidAdminCreation) {
		return err
	}
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) {
		if databaseError.ConstraintName == "admin_creation_authority" {
			return domain.ErrUnauthenticated
		}
		if databaseError.Code == "23505" && (databaseError.ConstraintName == "uk_strategy_instance_key" || databaseError.ConstraintName == "uk_admin_strategy_creation_key") {
			return domain.ErrAdminConflict
		}
	}
	return adminControlFailure(err)
}
