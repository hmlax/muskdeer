package data

import (
	"context"
	"time"

	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/permissionmenu"
)

type PermissionMenuRepo struct {
	log      *bLogger.Helper
	dbClient *store.DBClient
}

func NewPermissionMenuRepo(ctx *bootstrap.Context, dbClient *store.DBClient) *PermissionMenuRepo {
	return &PermissionMenuRepo{
		log:      ctx.NewLoggerHelper("permission-menu/repo/admin-service"),
		dbClient: dbClient,
	}
}

// CleanMenus 清理权限的所有菜单
func (r *PermissionMenuRepo) CleanMenus(
	ctx context.Context,
	tx *store.Tx,
	permissionIDs []uint32,
) error {
	if _, err := tx.PermissionMenu.Delete().
		Where(
			permissionmenu.PermissionIDIn(permissionIDs...),
		).
		Exec(ctx); err != nil {
		r.log.Errorf(context.Background(), "delete old permission menus failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("delete old permission menus failed")
	}
	return nil
}

// CleanNotExistMenus 清理权限中不存在的菜单
func (r *PermissionMenuRepo) CleanNotExistMenus(
	ctx context.Context,
	tx *store.Tx,
	permissionID uint32,
	menuIDs []uint32,
) error {
	if _, err := tx.PermissionMenu.Delete().
		Where(
			permissionmenu.MenuIDNotIn(menuIDs...),
			permissionmenu.PermissionIDEQ(permissionID),
		).
		Exec(ctx); err != nil {
		r.log.Errorf(context.Background(), "clean not exists permission menus failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("clean not exists permission menus failed")
	}
	return nil
}

// AssignMenus 给权限分配菜单
func (r *PermissionMenuRepo) AssignMenus(ctx context.Context, permissionID uint32, menuIDs []uint32) (err error) {
	var tx *store.Tx
	tx, err = r.dbClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("start transaction failed")
	}
	defer func() {
		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				r.log.Errorf(ctx, "transaction rollback failed: %s", rollbackErr.Error())
			}
			return
		}
		if commitErr := tx.Commit(); commitErr != nil {
			r.log.Errorf(ctx, "transaction commit failed: %s", commitErr.Error())
			err = operatorV1.ErrorPermissionInternalServerError("transaction commit failed")
		}
	}()

	// 清理失效关联失败不阻断分配（仅告警），理由同 CleanNotExistApis
	if err := r.CleanNotExistMenus(ctx, tx, permissionID, menuIDs); err != nil {
		r.log.Warnf(ctx, "clean not exist menus for permission [%d] failed: %s", permissionID, err.Error())
	}

	return r.AssignMenusWithTx(ctx, tx, permissionID, menuIDs)
}

// AssignMenusWithTx 给权限分配菜单
func (r *PermissionMenuRepo) AssignMenusWithTx(ctx context.Context, tx *store.Tx, permissionID uint32, menuIDs []uint32) error {
	if len(menuIDs) == 0 {
		return nil
	}

	now := time.Now()

	for _, menuID := range menuIDs {
		pm := tx.PermissionMenu.
			Create().
			SetPermissionID(permissionID).
			SetMenuID(menuID).
			SetCreatedAt(now).
			OnConflictColumns(
				permissionmenu.FieldPermissionID,
				permissionmenu.FieldMenuID,
			).
			UpdateNewValues().
			SetUpdatedAt(now)
		if err := pm.Exec(ctx); err != nil {
			r.log.Errorf(ctx, "assign permission menuIDs failed: %s", err.Error())
			return operatorV1.ErrorPermissionInternalServerError("assign permission menuIDs failed")
		}
	}

	return nil
}

// ListMenuIDs 列出权限关联的菜单ID列表
func (r *PermissionMenuRepo) ListMenuIDs(ctx context.Context, permissionIDs []uint32) ([]uint32, error) {
	q := r.dbClient.Client().PermissionMenu.
		Query().
		Where(
			permissionmenu.PermissionIDIn(permissionIDs...),
		)

	intIDs, err := q.
		Select(permissionmenu.FieldMenuID).
		Ints(ctx)
	if err != nil {
		r.log.Errorf(ctx, "list permission menus by permission id failed: %s", err.Error())
		return nil, operatorV1.ErrorPermissionInternalServerError("list permission menus by permission id failed")
	}

	ids := make([]uint32, len(intIDs))
	for i, v := range intIDs {
		ids[i] = uint32(v)
	}
	return ids, nil
}

// Truncate 清空表数据
func (r *PermissionMenuRepo) Truncate(ctx context.Context) error {
	builder := r.dbClient.Client().PermissionMenu.Delete().
		Where(
			permissionmenu.PermissionIDNotIn(1, 2, 3),
		)

	if _, err := builder.Exec(ctx); err != nil {
		r.log.Errorf(ctx, "failed to truncate permission menu table: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("truncate failed")
	}

	return nil
}

// Delete 删除权限关联的菜单
func (r *PermissionMenuRepo) Delete(ctx context.Context, permissionID uint32) error {
	if _, err := r.dbClient.Client().PermissionMenu.Delete().
		Where(
			permissionmenu.PermissionIDEQ(permissionID),
		).
		Exec(ctx); err != nil {
		r.log.Errorf(ctx, "failed to delete permission menu by permission id: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("delete failed")
	}

	return nil
}

func (r *PermissionMenuRepo) DeleteByPermissionIDs(ctx context.Context, permissionIDs []uint32) error {
	if _, err := r.dbClient.Client().PermissionMenu.Delete().
		Where(
			permissionmenu.PermissionIDIn(permissionIDs...),
		).
		Exec(ctx); err != nil {
		r.log.Errorf(ctx, "delete permission menus by permission ids failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("delete permission menus by permission ids failed")
	}
	return nil
}

// AssignMenu 给权限分配菜单
func (r *PermissionMenuRepo) AssignMenu(ctx context.Context, permissionID uint32, menuID uint32) error {
	now := time.Now()

	pm := r.dbClient.Client().PermissionMenu.
		Create().
		SetPermissionID(permissionID).
		SetMenuID(menuID).
		SetCreatedAt(now).
		OnConflictColumns(
			permissionmenu.FieldPermissionID,
			permissionmenu.FieldMenuID,
		).
		UpdateNewValues().
		SetUpdatedAt(now)
	if err := pm.Exec(ctx); err != nil {
		return operatorV1.ErrorPermissionInternalServerError("assign permission menu failed")
	}

	return nil
}
