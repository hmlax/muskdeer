package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategyconfig"
)

type adminStrategyPublisher struct{ db *gorm.DB }

// NewAdminStrategyPublisher borrows storage without claiming runtime ownership.
func NewAdminStrategyPublisher(db *gorm.DB) (domain.AdminStrategyPublisher, error) {
	if db == nil {
		return nil, domain.ErrInvalidAdminPublication
	}
	return &adminStrategyPublisher{db: db}, nil
}

func (s *adminStrategyPublisher) Publish(ctx context.Context, token, csrf string, request domain.AdminPublishStrategy) (domain.AdminPublicationReceipt, error) {
	if err := coreContext(ctx); err != nil {
		return domain.AdminPublicationReceipt{}, err
	}
	request, err := request.Normalize()
	if err != nil {
		return domain.AdminPublicationReceipt{}, err
	}
	parameters, err := strategyconfig.Encode(request.Parameters)
	if err != nil {
		return domain.AdminPublicationReceipt{}, domain.ErrInvalidAdminPublication
	}
	request.Parameters, err = strategyconfig.Decode(domain.ParametersSchemaVersion, parameters)
	if err != nil {
		return domain.AdminPublicationReceipt{}, domain.ErrInvalidAdminPublication
	}
	fingerprint, err := adminPublicationFingerprint(request, parameters)
	if err != nil {
		return domain.AdminPublicationReceipt{}, adminPublicationFailure(err)
	}
	var result domain.AdminPublicationReceipt
	err = s.db.WithContext(ctx).Model(&modelv1.AdminStrategyPublication{}).Transaction(func(tx *gorm.DB) error {
		user, session, err := readAdminControlAuthority(ctx, tx, token, csrf)
		if err != nil {
			return err
		}
		var existing modelv1.AdminStrategyPublication
		err = tx.WithContext(ctx).Model(&modelv1.AdminStrategyPublication{}).
			Where("actor_user_id = ? AND idempotency_key = ?", user.Id, request.Key).Take(&existing).Error
		if err == nil {
			if existing.RequestFingerprint != fingerprint {
				return domain.ErrAdminConflict
			}
			result = adminPublicationReceipt(&existing)
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var instance modelv1.StrategyInstance
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ? AND delete_at IS NULL", request.StrategyID).
			Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).Take(&instance).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrAdminNotFound
			}
			return err
		}
		if instance.Revision != request.ExpectedRevision || instance.RequestedConfigVersion != request.ExpectedConfigVersion {
			return domain.ErrAdminConflict
		}
		if instance.Template != string(request.Parameters.Template) {
			return domain.ErrInvalidAdminPublication
		}
		// Identity/mode/code remain persisted facts. Reuse the same provisioned
		// instrument -> account -> budget -> portfolio -> selected-feed checks.
		if err := lockCreationDependencies(ctx, tx, domain.AdminCreateStrategy{
			AccountID:    instance.AccountId,
			InstrumentID: instance.InstrumentId,
			Source:       request.Source,
			Mode:         domain.MarketMode(instance.MarketMode),
			Parameters:   request.Parameters,
		}); err != nil {
			return err
		}
		var latest modelv1.StrategyConfigVersion
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Select("Version").
			Where("instance_id = ?", instance.Id).Order("version DESC").Take(&latest).Error; err != nil {
			return err
		}
		if latest.Version == math.MaxInt64 {
			return domain.ErrAdminConflict
		}
		version := &modelv1.StrategyConfigVersion{
			InstanceId:              instance.Id,
			Version:                 latest.Version + 1,
			MarketDataSource:        string(request.Source),
			CodeVersion:             instance.CodeVersion,
			CreatedBy:               user.Username,
			ParametersSchemaVersion: domain.ParametersSchemaVersion,
			Template:                proto.String(instance.Template),
			MakerParameters:         parameters.GetMarketMaker(),
			GridParameters:          parameters.GetGrid(),
		}
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Create(version).Error; err != nil {
			return err
		}
		alive := tx.WithContext(ctx).Model(&modelv1.AdminSession{}).Select("1").
			Where("id = ? AND user_id = ? AND credential_version = ? AND revoked_at IS NULL AND delete_at IS NULL AND expires_at > floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint", session.Id, user.Id, user.CredentialVersion)
		changed := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ? AND revision = ? AND EXISTS (?)", instance.Id, instance.Revision, alive).
			Select("RequestedConfigVersion", "MarketDataSource", "Revision").Updates(&modelv1.StrategyInstance{
			RequestedConfigVersion: version.Version,
			MarketDataSource:       string(request.Source),
			Revision:               instance.Revision + 1,
		})
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return domain.ErrUnauthenticated
		}
		if err := invalidateLifecycleRisk(ctx, tx, &instance); err != nil {
			return err
		}
		previousVersion := instance.RequestedConfigVersion
		instance.RequestedConfigVersion, instance.MarketDataSource = version.Version, string(request.Source)
		if err := supersedeAdminControls(ctx, tx, user.Id, &instance, domain.StrategyState(instance.DesiredState)); err != nil {
			return err
		}
		audit := &modelv1.AdminAudit{
			Actor:         user.Username,
			ActorRole:     user.Role,
			Action:        domain.AdminStrategyPublish,
			Resource:      domain.AdminAuditResourceStrategy + ":" + strconv.FormatInt(instance.Id, 10),
			Reason:        request.Reason,
			BeforeVersion: proto.Int64(instance.Revision),
			AfterVersion:  proto.Int64(instance.Revision + 1),
		}
		if err := tx.WithContext(ctx).Model(&modelv1.AdminAudit{}).Create(audit).Error; err != nil {
			return err
		}
		receipt := &modelv1.AdminStrategyPublication{
			ActorUserId:           user.Id,
			ActorSessionId:        session.Id,
			StrategyInstanceId:    instance.Id,
			AuditId:               audit.Id,
			IdempotencyKey:        request.Key,
			RequestFingerprint:    fingerprint,
			PreviousConfigVersion: previousVersion,
			ConfigVersion:         version.Version,
			PreviousRevision:      instance.Revision,
			Revision:              instance.Revision + 1,
			MarketDataSource:      string(request.Source),
			Reason:                request.Reason,
		}
		if err := tx.WithContext(ctx).Model(&modelv1.AdminStrategyPublication{}).Create(receipt).Error; err != nil {
			return err
		}
		result = adminPublicationReceipt(receipt)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return domain.AdminPublicationReceipt{}, adminPublicationFailure(err)
	}
	return result, nil
}

func adminPublicationReceipt(row *modelv1.AdminStrategyPublication) domain.AdminPublicationReceipt {
	return domain.AdminPublicationReceipt{
		ID:                    row.Id,
		ActorUserID:           row.ActorUserId,
		StrategyID:            row.StrategyInstanceId,
		PreviousConfigVersion: row.PreviousConfigVersion,
		ConfigVersion:         row.ConfigVersion,
		PreviousRevision:      row.PreviousRevision,
		Revision:              row.Revision,
		Source:                domain.MarketSource(row.MarketDataSource),
		Reason:                row.Reason,
		CreatedAt:             time.UnixMilli(row.CreateAt).UTC(),
	}
}

func adminPublicationFingerprint(request domain.AdminPublishStrategy, parameters *commonv1.StrategyParameters) (string, error) {
	encoded, err := (proto.MarshalOptions{Deterministic: true}).Marshal(parameters)
	if err != nil {
		return "", err
	}
	data := binary.BigEndian.AppendUint64(nil, uint64(request.StrategyID))
	data = binary.BigEndian.AppendUint64(data, uint64(request.ExpectedRevision))
	data = binary.BigEndian.AppendUint64(data, uint64(request.ExpectedConfigVersion))
	for _, field := range []string{domain.AdminStrategyPublish, string(request.Source), request.Reason, string(encoded)} {
		data = binary.BigEndian.AppendUint32(data, uint32(len(field)))
		data = append(data, field...)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func adminPublicationFailure(err error) error {
	if errors.Is(err, domain.ErrInvalidAdminPublication) {
		return err
	}
	if errors.Is(err, domain.ErrInvalidAdminCreation) {
		return domain.ErrInvalidAdminPublication
	}
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) {
		if databaseError.ConstraintName == "admin_publication_authority" {
			return domain.ErrUnauthenticated
		}
		if databaseError.Code == "23505" && (databaseError.ConstraintName == "uk_admin_strategy_publication_key" || databaseError.ConstraintName == "uk_admin_strategy_publication_version" || databaseError.ConstraintName == "uk_strategy_config_instance_version") {
			return domain.ErrAdminConflict
		}
	}
	return adminControlFailure(err)
}

var _ domain.AdminStrategyPublisher = (*adminStrategyPublisher)(nil)
