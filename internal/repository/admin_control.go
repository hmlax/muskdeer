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
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type adminStrategyControl struct{ db *gorm.DB }

// NewAdminStrategyControl borrows storage without starting work, granting risk,
// creating accounts or changing actual runtime state.
func NewAdminStrategyControl(db *gorm.DB) (domain.AdminStrategyControl, error) {
	if db == nil {
		return nil, domain.ErrInvalidAdminControl
	}
	return &adminStrategyControl{db: db}, nil
}

func (s *adminStrategyControl) Control(ctx context.Context, token, csrf string, request domain.AdminControlRequest) (domain.AdminOperation, error) {
	if err := coreContext(ctx); err != nil {
		return domain.AdminOperation{}, err
	}
	request, err := request.Normalize()
	if err != nil {
		return domain.AdminOperation{}, err
	}
	fingerprint := adminControlFingerprint(request)
	var result domain.AdminOperation
	err = s.db.WithContext(ctx).Model(&modelv1.AdminStrategyOperation{}).Transaction(func(tx *gorm.DB) error {
		user, session, err := readAdminControlAuthority(ctx, tx, token, csrf)
		if err != nil {
			return err
		}
		var existing modelv1.AdminStrategyOperation
		err = tx.WithContext(ctx).Model(&modelv1.AdminStrategyOperation{}).
			Where("actor_user_id = ? AND idempotency_key = ?", user.Id, request.Key).Take(&existing).Error
		if err == nil {
			if existing.RequestFingerprint != fingerprint {
				return domain.ErrAdminConflict
			}
			result = adminOperation(&existing)
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var instance modelv1.StrategyInstance
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).
			Where("id = ? AND delete_at IS NULL", request.StrategyID).
			Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).Take(&instance).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrAdminNotFound
			}
			return err
		}
		if instance.Revision != request.ExpectedRevision || !adminControlAllowed(request.Action, domain.StrategyState(instance.State)) {
			return domain.ErrAdminConflict
		}
		if request.Action == domain.AdminControlStart || request.Action == domain.AdminControlResume {
			config, err := adminVersion(ctx, tx, &instance, instance.RequestedConfigVersion, instance.MarketDataSource)
			if err != nil {
				return err
			}
			if config.SchemaVersion != domain.ParametersSchemaVersion || config.CodeVersion != instance.CodeVersion {
				return domain.ErrInvalidAdminControl
			}
		}
		target, _ := request.Action.Target()
		alive := tx.WithContext(ctx).Model(&modelv1.AdminSession{}).Select("1").
			Where("id = ? AND user_id = ? AND credential_version = ? AND revoked_at IS NULL AND delete_at IS NULL AND expires_at > floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint", session.Id, user.Id, user.CredentialVersion)
		changed := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).
			Where("id = ? AND revision = ? AND EXISTS (?)", instance.Id, instance.Revision, alive).
			Select("DesiredState", "DesiredRunning", "Revision").Updates(&modelv1.StrategyInstance{
			DesiredState:   string(target),
			DesiredRunning: target == domain.StrategyStateRunning,
			Revision:       instance.Revision + 1,
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
		if err := supersedeAdminControls(ctx, tx, user.Id, &instance, target); err != nil {
			return err
		}
		audit := &modelv1.AdminAudit{
			Actor:         user.Username,
			ActorRole:     user.Role,
			Action:        string(request.Action),
			Resource:      domain.AdminAuditResourceStrategy + ":" + strconv.FormatInt(instance.Id, 10),
			Reason:        request.Reason,
			BeforeVersion: proto.Int64(instance.Revision),
			AfterVersion:  proto.Int64(instance.Revision + 1),
		}
		if err := tx.WithContext(ctx).Model(&modelv1.AdminAudit{}).Create(audit).Error; err != nil {
			return err
		}
		operation := &modelv1.AdminStrategyOperation{
			ActorUserId:        user.Id,
			ActorSessionId:     session.Id,
			StrategyInstanceId: instance.Id,
			AuditId:            audit.Id,
			IdempotencyKey:     request.Key,
			RequestFingerprint: fingerprint,
			Action:             string(request.Action),
			TargetState:        string(target),
			ExpectedRevision:   instance.Revision,
			EffectRevision:     instance.Revision + 1,
			ConfigVersion:      instance.RequestedConfigVersion,
			MarketDataSource:   instance.MarketDataSource,
			Reason:             request.Reason,
			State:              string(domain.AdminOperationAccepted),
			OutcomeCode:        domain.AdminOperationOutcomePending,
			Revision:           1,
		}
		if err := tx.WithContext(ctx).Model(&modelv1.AdminStrategyOperation{}).Create(operation).Error; err != nil {
			return err
		}
		result = adminOperation(operation)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return domain.AdminOperation{}, adminControlFailure(err)
	}
	return result, nil
}

func (s *adminStrategyControl) Operation(ctx context.Context, token string, id int64) (domain.AdminOperation, error) {
	if err := coreContext(ctx); err != nil {
		return domain.AdminOperation{}, err
	}
	if id <= 0 {
		return domain.AdminOperation{}, domain.ErrInvalidAdminControl
	}
	var result domain.AdminOperation
	err := s.db.WithContext(ctx).Model(&modelv1.AdminStrategyOperation{}).Transaction(func(tx *gorm.DB) error {
		if _, _, err := readAdminAuthority(ctx, tx, token, "", domain.AdminRoleViewer, ""); err != nil {
			return err
		}
		var row modelv1.AdminStrategyOperation
		if err := tx.WithContext(ctx).Model(&modelv1.AdminStrategyOperation{}).Where("id = ?", id).Take(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrAdminNotFound
			}
			return err
		}
		result = adminOperation(&row)
		return nil
	}, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return domain.AdminOperation{}, adminControlFailure(err)
	}
	return result, nil
}

func adminControlAllowed(action domain.AdminControlAction, state domain.StrategyState) bool {
	switch action {
	case domain.AdminControlStart:
		return state == domain.StrategyStateCreated || state == domain.StrategyStateStopped
	case domain.AdminControlResume:
		return state == domain.StrategyStatePaused
	case domain.AdminControlPause:
		return state == domain.StrategyStateRunning || state == domain.StrategyStateStarting || state == domain.StrategyStateRecovering
	case domain.AdminControlStop:
		return state == domain.StrategyStateCreated || state == domain.StrategyStateStarting || state == domain.StrategyStateRecovering || state == domain.StrategyStateRunning || state == domain.StrategyStatePausing || state == domain.StrategyStatePaused || state == domain.StrategyStateStopping || state == domain.StrategyStateStopped || state == domain.StrategyStateError
	default:
		return false
	}
}

// Record the intermediate replacement while the strategy row is still locked.
// An asynchronous observer cannot reconstruct a target that changed and changed
// back between polls. Audit/receipt failure rolls this result back as well.
func supersedeAdminControls(ctx context.Context, tx *gorm.DB, userID int64, instance *modelv1.StrategyInstance, target domain.StrategyState) error {
	now, err := adminDatabaseTime(ctx, tx, userID)
	if err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&modelv1.AdminStrategyOperation{}).
		Where("strategy_instance_id = ? AND state = ? AND (target_state <> ? OR config_version <> ? OR market_data_source <> ?)", instance.Id, domain.AdminOperationAccepted, target, instance.RequestedConfigVersion, instance.MarketDataSource).
		Select("State", "OutcomeCode", "Revision", "CompletedAt", "ObservedRevision", "ObservedState", "ObservedOwnerId", "ObservedOwnerEpoch").
		Updates(&modelv1.AdminStrategyOperation{
			State:              string(domain.AdminOperationSuperseded),
			OutcomeCode:        domain.AdminOperationOutcomeReplaced,
			Revision:           2,
			CompletedAt:        proto.Int64(now),
			ObservedRevision:   proto.Int64(instance.Revision + 1),
			ObservedState:      proto.String(instance.State),
			ObservedOwnerId:    proto.String(instance.RuntimeOwnerId),
			ObservedOwnerEpoch: proto.Int64(instance.RuntimeOwnerEpoch),
		}).Error
}

func adminControlFingerprint(request domain.AdminControlRequest) string {
	data := binary.BigEndian.AppendUint64(nil, uint64(request.StrategyID))
	data = binary.BigEndian.AppendUint64(data, uint64(request.ExpectedRevision))
	for _, field := range []string{string(request.Action), request.Reason} {
		data = binary.BigEndian.AppendUint32(data, uint32(len(field)))
		data = append(data, field...)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func adminOperation(row *modelv1.AdminStrategyOperation) domain.AdminOperation {
	result := domain.AdminOperation{
		ID:               row.Id,
		ActorUserID:      row.ActorUserId,
		StrategyID:       row.StrategyInstanceId,
		Action:           domain.AdminControlAction(row.Action),
		Target:           domain.StrategyState(row.TargetState),
		ExpectedRevision: row.ExpectedRevision,
		EffectRevision:   row.EffectRevision,
		ConfigVersion:    row.ConfigVersion,
		Source:           domain.MarketSource(row.MarketDataSource),
		State:            domain.AdminOperationState(row.State),
		Outcome:          row.OutcomeCode,
		Reason:           row.Reason,
		Revision:         row.Revision,
		CreatedAt:        time.UnixMilli(row.CreateAt).UTC(),
	}
	if row.CompletedAt != nil {
		result.CompletedAt = time.UnixMilli(*row.CompletedAt).UTC()
		result.Observation = &domain.AdminOperationObservation{
			StrategyRevision:  row.GetObservedRevision(),
			State:             domain.StrategyState(row.GetObservedState()),
			RuntimeOwnerID:    row.GetObservedOwnerId(),
			RuntimeOwnerEpoch: row.GetObservedOwnerEpoch(),
		}
	}
	return result
}

func adminControlFailure(err error) error {
	if err == nil {
		return nil
	}
	for _, known := range []error{domain.ErrUnauthenticated, domain.ErrForbidden, domain.ErrInvalidAdminControl, domain.ErrAdminConflict, domain.ErrAdminNotFound, context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, known) {
			return err
		}
	}
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) && databaseError.ConstraintName == "admin_operation_authority" {
		return domain.ErrUnauthenticated
	}
	return &adminControlError{cause: err}
}

type adminControlError struct{ cause error }

func (*adminControlError) Error() string { return domain.MessageAdminControlUnavailable }
func (e *adminControlError) Unwrap() error {
	return errors.Join(domain.ErrAdminControlUnavailable, e.cause)
}

var _ domain.AdminStrategyControl = (*adminStrategyControl)(nil)
