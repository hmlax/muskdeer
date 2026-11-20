package data

import (
	"context"
	"time"

	"github.com/tx7do/go-utils/mapper"
	"github.com/tx7do/go-utils/timeutil"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/userposition"
)

type UserPositionRepo struct {
	log             *bLogger.Helper
	dbClient        *store.DBClient
	statusConverter *mapper.EnumTypeConverter[operatorV1.UserPosition_Status, userposition.Status]
}

func NewUserPositionRepo(ctx *bootstrap.Context, dbClient *store.DBClient) *UserPositionRepo {
	return &UserPositionRepo{
		log:      ctx.NewLoggerHelper("user-position/repo/admin-service"),
		dbClient: dbClient,
		statusConverter: mapper.NewEnumTypeConverter[operatorV1.UserPosition_Status, userposition.Status](
			operatorV1.UserPosition_Status_name,
			operatorV1.UserPosition_Status_value,
		),
	}
}

// CleanRelationsByUserID 删除用户的所有岗位关联
func (r *UserPositionRepo) CleanRelationsByUserID(ctx context.Context, tx *store.Tx, userID uint32) error {
	if userID == 0 {
		return nil
	}

	if _, err := tx.UserPosition.Delete().
		Where(
			userposition.UserIDEQ(userID),
		).
		Exec(ctx); err != nil {
		r.log.Errorf(ctx, "delete old user positions failed: %s", err.Error())
		return operatorV1.ErrorIdentityInternalServerError("delete old user positions failed")
	}
	return nil
}

// CleanRelationsByUserIDs 删除多个用户的所有岗位关联
func (r *UserPositionRepo) CleanRelationsByUserIDs(ctx context.Context, tx *store.Tx, userIDs []uint32) error {
	if len(userIDs) == 0 {
		return nil
	}

	if _, err := tx.UserPosition.Delete().
		Where(
			userposition.UserIDIn(userIDs...),
		).
		Exec(ctx); err != nil {
		r.log.Errorf(ctx, "delete old user positions by user ids failed: %s", err.Error())
		return operatorV1.ErrorIdentityInternalServerError("delete old user positions by user ids failed")
	}
	return nil
}

// CleanRelationsByPositionID 删除岗位的所有用户关联
func (r *UserPositionRepo) CleanRelationsByPositionID(ctx context.Context, tx *store.Tx, positionID uint32) error {
	if positionID == 0 {
		return nil
	}

	if _, err := tx.UserPosition.Delete().
		Where(
			userposition.PositionIDEQ(positionID),
		).
		Exec(ctx); err != nil {
		r.log.Errorf(ctx, "delete old user positions by position id failed: %s", err.Error())
		return operatorV1.ErrorIdentityInternalServerError("delete old user positions by position id failed")
	}
	return nil
}

// CleanRelationsByPositionIDs 删除多个岗位的所有用户关联
func (r *UserPositionRepo) CleanRelationsByPositionIDs(ctx context.Context, tx *store.Tx, positionIDs []uint32) error {
	if len(positionIDs) == 0 {
		return nil
	}

	if _, err := tx.UserPosition.Delete().
		Where(
			userposition.PositionIDIn(positionIDs...),
		).
		Exec(ctx); err != nil {
		r.log.Errorf(ctx, "delete old user positions by position ids failed: %s", err.Error())
		return operatorV1.ErrorIdentityInternalServerError("delete old user positions by position ids failed")
	}
	return nil
}

// RemovePositionsFromUser 从用户移除岗位
func (r *UserPositionRepo) RemovePositionsFromUser(ctx context.Context, userID uint32, positionIDs []uint32) error {
	if len(positionIDs) == 0 || userID == 0 {
		return nil
	}

	_, err := r.dbClient.Client().UserPosition.Delete().
		Where(
			userposition.And(
				userposition.UserIDEQ(userID),
				userposition.PositionIDIn(positionIDs...),
			),
		).
		Exec(ctx)
	if err != nil {
		r.log.Errorf(ctx, "remove positions from user failed: %s", err.Error())
		return operatorV1.ErrorIdentityInternalServerError("remove positions from user failed")
	}
	return nil
}
func (r *UserPositionRepo) AssignUserPosition(
	ctx context.Context,
	tx *store.Tx,
	data *operatorV1.UserPosition,
) error {
	if data == nil {
		return nil
	}

	now := time.Now()

	rm := tx.UserPosition.
		Create().
		SetUserID(data.GetUserId()).
		SetPositionID(data.GetPositionId()).
		SetNillableStatus(r.statusConverter.ToEntity(data.Status)).
		SetNillableAssignedBy(data.AssignedBy).
		SetNillableAssignedAt(timeutil.TimestamppbToTime(data.AssignedAt)).
		SetNillableIsPrimary(data.IsPrimary).
		SetNillableStartAt(timeutil.TimestamppbToTime(data.StartAt)).
		SetNillableEndAt(timeutil.TimestamppbToTime(data.EndAt)).
		SetNillableCreatedBy(data.CreatedBy).
		SetCreatedAt(now)

	_, err := rm.Save(ctx)
	if err != nil {
		r.log.Errorf(ctx, "assign position to user failed: %s", err.Error())
		return operatorV1.ErrorIdentityInternalServerError("assign position to user failed")
	}

	return nil
}

// AssignUserPositions 分配岗位给用户
func (r *UserPositionRepo) AssignUserPositions(
	ctx context.Context, tx *store.Tx,
	userID uint32,
	datas []*operatorV1.UserPosition,
) error {
	if len(datas) == 0 || userID == 0 {
		return nil
	}

	var err error

	// 删除该用户的所有旧关联
	if err = r.CleanRelationsByUserID(ctx, tx, userID); err != nil {
		return operatorV1.ErrorIdentityInternalServerError("clean old user positions failed")
	}

	now := time.Now()

	var userPositionCreates []*store.UserPositionCreate
	for _, data := range datas {
		if data.StartAt == nil {
			data.StartAt = timeutil.TimeToTimestamppb(&now)
		}
		rm := tx.UserPosition.
			Create().
			SetNillableTenantID(data.TenantId).
			SetUserID(userID).
			SetPositionID(data.GetPositionId()).
			SetNillableStatus(r.statusConverter.ToEntity(data.Status)).
			SetNillableAssignedBy(data.AssignedBy).
			SetNillableAssignedAt(timeutil.TimestamppbToTime(data.AssignedAt)).
			SetNillableIsPrimary(data.IsPrimary).
			SetNillableStartAt(timeutil.TimestamppbToTime(data.StartAt)).
			SetNillableEndAt(timeutil.TimestamppbToTime(data.EndAt)).
			SetNillableCreatedBy(data.CreatedBy).
			SetCreatedAt(now)
		userPositionCreates = append(userPositionCreates, rm)
	}

	_, err = tx.UserPosition.CreateBulk(userPositionCreates...).Save(ctx)
	if err != nil {
		r.log.Errorf(ctx, "assign positions to user failed: %s", err.Error())
		return operatorV1.ErrorIdentityInternalServerError("assign positions to user failed")
	}

	return nil
}

// ListPositionIDs 获取用户的岗位ID列表
func (r *UserPositionRepo) ListPositionIDs(ctx context.Context, userID uint32, excludeExpired bool) ([]uint32, error) {
	if userID == 0 {
		return []uint32{}, nil
	}

	q := r.dbClient.Client().UserPosition.Query().
		Where(
			userposition.UserIDEQ(userID),
		)

	if excludeExpired {
		now := time.Now()
		q = q.Where(
			userposition.Or(
				userposition.EndAtIsNil(),
				userposition.EndAtGT(now),
			),
		)
	}

	intIDs, err := q.
		Select(userposition.FieldPositionID).
		Ints(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query position ids by user id failed: %s", err.Error())
		return nil, operatorV1.ErrorIdentityInternalServerError("query position ids by user id failed")
	}
	ids := make([]uint32, len(intIDs))
	for i, v := range intIDs {
		ids[i] = uint32(v)
	}
	return ids, nil
}

// ListUserIDs 获取岗位关联的用户ID列表
func (r *UserPositionRepo) ListUserIDs(ctx context.Context, positionID uint32, excludeExpired bool) ([]uint32, error) {
	if positionID == 0 {
		return []uint32{}, nil
	}

	q := r.dbClient.Client().UserPosition.Query().
		Where(
			userposition.PositionIDEQ(positionID),
		)

	if excludeExpired {
		now := time.Now()
		q = q.Where(
			userposition.Or(
				userposition.EndAtIsNil(),
				userposition.EndAtGT(now),
			),
		)
	}

	intIDs, err := q.
		Select(userposition.FieldUserID).
		Ints(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query user ids by position id failed: %s", err.Error())
		return nil, operatorV1.ErrorIdentityInternalServerError("query user ids by position id failed")
	}
	ids := make([]uint32, len(intIDs))
	for i, v := range intIDs {
		ids[i] = uint32(v)
	}
	return ids, nil
}

// ListUserIDsByPositionIDs 获取多个岗位关联的用户ID列表
func (r *UserPositionRepo) ListUserIDsByPositionIDs(ctx context.Context, positionIDs []uint32, excludeExpired bool) ([]uint32, error) {
	if len(positionIDs) == 0 {
		return nil, nil
	}

	q := r.dbClient.Client().UserPosition.Query().
		Where(
			userposition.PositionIDIn(positionIDs...),
		)

	if excludeExpired {
		now := time.Now()
		q = q.Where(
			userposition.Or(
				userposition.EndAtIsNil(),
				userposition.EndAtGT(now),
			),
		)
	}

	intIDs, err := q.
		Select(userposition.FieldUserID).
		Ints(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query user ids by position ids failed: %s", err.Error())
		return nil, operatorV1.ErrorIdentityInternalServerError("query user ids by position ids failed")
	}
	ids := make([]uint32, len(intIDs))
	for i, v := range intIDs {
		ids[i] = uint32(v)
	}
	return ids, nil
}
