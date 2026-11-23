package data

import (
	"context"
	"time"

	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/permissionapi"
)

// PermissionApiRepo stores permission associations with administration APIs.
type PermissionApiRepo struct {
	log      *bLogger.Helper
	dbClient *store.DBClient
}

// NewPermissionApiRepo borrows the administration database dependencies.
func NewPermissionApiRepo(ctx *bootstrap.Context, dbClient *store.DBClient) *PermissionApiRepo {
	return &PermissionApiRepo{
		log:      ctx.NewLoggerHelper("permission-api/repo/admin-service"),
		dbClient: dbClient,
	}
}

// CleanApis 清理权限的所有API资源
func (r *PermissionApiRepo) CleanApis(
	ctx context.Context,
	tx *store.Tx,
	permissionIDs []uint32,
) error {
	if _, err := tx.PermissionApi.Delete().
		Where(
			permissionapi.PermissionIDIn(permissionIDs...),
		).
		Exec(ctx); err != nil {
		r.log.Errorf(context.Background(), "delete old permission apis failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("delete old permission apis failed")
	}
	return nil
}

// CleanNotExistApis 清理权限中不存在的API资源
func (r *PermissionApiRepo) CleanNotExistApis(
	ctx context.Context,
	tx *store.Tx,
	permissionID uint32,
	apiIDs []uint32,
) error {
	if _, err := tx.PermissionApi.Delete().
		Where(
			permissionapi.APIIDNotIn(apiIDs...),
			permissionapi.PermissionIDEQ(permissionID),
		).
		Exec(ctx); err != nil {
		r.log.Errorf(context.Background(), "clean not exists permission apis failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("clean not exists permission apis failed")
	}
	return nil
}

// AssignApis 给权限分配API资源
func (r *PermissionApiRepo) AssignApis(
	ctx context.Context,
	permissionID uint32,
	apiIDs []uint32,
) (err error) {
	var tx *store.Tx
	tx, err = r.dbClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(context.Background(), "start transaction failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("start transaction failed")
	}
	defer func() {
		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				r.log.Errorf(context.Background(), "transaction rollback failed: %s", rollbackErr.Error())
			}
			return
		}
		if commitErr := tx.Commit(); commitErr != nil {
			r.log.Errorf(context.Background(), "transaction commit failed: %s", commitErr.Error())
			err = operatorV1.ErrorPermissionInternalServerError("transaction commit failed")
		}
	}()

	// 清理失效关联失败不阻断分配（仅告警）：CleanNotExist 出错时仍执行 Assign，
	// 由 (tenant, permission, api) 唯一约束兜底重复数据
	if err := r.CleanNotExistApis(ctx, tx, permissionID, apiIDs); err != nil {
		r.log.Warnf(context.Background(), "clean not exist apis for permission [%d] failed: %s", permissionID, err.Error())
	}

	return r.AssignApisWithTx(ctx, tx, permissionID, apiIDs)
}

// AssignApisWithTx 给权限分配API资源
func (r *PermissionApiRepo) AssignApisWithTx(
	ctx context.Context,
	tx *store.Tx,
	permissionID uint32,
	apis []uint32,
) error {
	if len(apis) == 0 {
		return nil
	}

	now := time.Now()

	for _, apiID := range apis {
		pm := tx.PermissionApi.
			Create().
			SetPermissionID(permissionID).
			SetAPIID(apiID).
			SetCreatedAt(now).
			OnConflictColumns(
				permissionapi.FieldPermissionID,
				permissionapi.FieldAPIID,
			).
			UpdateNewValues().
			SetUpdatedAt(now)
		if err := pm.Exec(ctx); err != nil {
			r.log.Errorf(context.Background(), "assign permission apis failed: %s", err.Error())
			return operatorV1.ErrorPermissionInternalServerError("assign permission apis failed")
		}
	}

	return nil
}

// ListApiIDs 列出权限关联的API资源ID列表
func (r *PermissionApiRepo) ListApiIDs(ctx context.Context, permissionIDs []uint32) ([]uint32, error) {
	q := r.dbClient.Client().PermissionApi.
		Query().
		Where(
			permissionapi.PermissionIDIn(permissionIDs...),
		)

	intIDs, err := q.
		Select(permissionapi.FieldAPIID).
		Ints(ctx)
	if err != nil {
		r.log.Errorf(ctx, "list permission apis by permission id failed: %s", err.Error())
		return nil, operatorV1.ErrorPermissionInternalServerError("list permission apis by permission id failed")
	}

	ids := make([]uint32, len(intIDs))
	for i, v := range intIDs {
		ids[i] = uint32(v)
	}
	return ids, nil
}

// Truncate 清空表数据
func (r *PermissionApiRepo) Truncate(ctx context.Context) error {
	builder := r.dbClient.Client().PermissionApi.Delete().
		Where(
			permissionapi.PermissionIDNotIn(1, 2, 3),
		)

	if _, err := builder.Exec(ctx); err != nil {
		r.log.Errorf(ctx, "failed to truncate permission api table: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("truncate failed")
	}

	return nil
}

// Delete 删除权限关联的API资源
func (r *PermissionApiRepo) Delete(ctx context.Context, permissionID uint32) error {
	if _, err := r.dbClient.Client().PermissionApi.Delete().
		Where(
			permissionapi.PermissionIDEQ(permissionID),
		).
		Exec(ctx); err != nil {
		r.log.Errorf(ctx, "delete permission apis by permission id failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("delete permission apis by permission id failed")
	}
	return nil
}

func (r *PermissionApiRepo) DeleteByPermissionIDs(ctx context.Context, permissionIDs []uint32) error {
	if _, err := r.dbClient.Client().PermissionApi.Delete().
		Where(
			permissionapi.PermissionIDIn(permissionIDs...),
		).
		Exec(ctx); err != nil {
		r.log.Errorf(ctx, "delete permission apis by permission ids failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("delete permission apis by permission ids failed")
	}
	return nil
}

// AssignApi 给权限分配API资源
func (r *PermissionApiRepo) AssignApi(ctx context.Context, permissionID uint32, apiID uint32) error {
	now := time.Now()
	pm := r.dbClient.Client().PermissionApi.
		Create().
		SetPermissionID(permissionID).
		SetAPIID(apiID).
		SetCreatedAt(now).
		OnConflictColumns(
			permissionapi.FieldPermissionID,
			permissionapi.FieldAPIID,
		).
		UpdateNewValues().
		SetUpdatedAt(now)
	if err := pm.Exec(ctx); err != nil {
		return operatorV1.ErrorPermissionInternalServerError("assign permission api failed")
	}

	return nil
}
