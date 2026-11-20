package data

import (
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/hmlax/muskdeer/operator/internal/data/store/queryscope"

	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"

	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/permission"

	"github.com/tx7do/go-utils/copierutil"
	"github.com/tx7do/go-utils/mapper"

	"github.com/hmlax/muskdeer/deer/management/constants"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"

	"github.com/tx7do/go-utils/sliceutil"
)

type PermissionRepo struct {
	dbClient *store.DBClient
	log      *bLogger.Helper

	mapper          *mapper.CopierMapper[operatorV1.Permission, store.Permission]
	statusConverter *mapper.EnumTypeConverter[operatorV1.Permission_Status, permission.Status]

	repository *store.Repository[operatorV1.Permission, store.Permission]

	permissionApiRepo  *PermissionApiRepo
	permissionMenuRepo *PermissionMenuRepo
}

func NewPermissionRepo(
	ctx *bootstrap.Context,
	dbClient *store.DBClient,
	permissionApiRepo *PermissionApiRepo,
	permissionMenuRepo *PermissionMenuRepo,
) *PermissionRepo {
	repo := &PermissionRepo{
		log:      ctx.NewLoggerHelper("permission/repo/admin-service"),
		dbClient: dbClient,
		mapper:   mapper.NewCopierMapper[operatorV1.Permission, store.Permission](),
		statusConverter: mapper.NewEnumTypeConverter[operatorV1.Permission_Status, permission.Status](
			operatorV1.Permission_Status_name, operatorV1.Permission_Status_value,
		),
		permissionApiRepo:  permissionApiRepo,
		permissionMenuRepo: permissionMenuRepo,
	}

	repo.init()

	return repo
}

func (r *PermissionRepo) init() {
	r.repository = store.NewRepository[operatorV1.Permission, store.Permission](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())

	r.mapper.AppendConverters(r.statusConverter.NewConverterPair())
}

func (r *PermissionRepo) Count(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.CountPermissionResponse, error) {
	builder := r.dbClient.Client().Permission.Query()

	whereSelectors, _, _ := r.repository.BuildListSelectorWithPaging(builder, req)
	if len(whereSelectors) != 0 {
		builder.Modify(whereSelectors...)
	}

	count, err := builder.Count(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query permission count failed: %s", err.Error())
		return nil, operatorV1.ErrorPermissionInternalServerError("query permission count failed")
	}

	return &operatorV1.CountPermissionResponse{
		Count: uint64(count),
	}, nil
}

func clearFilterExprByFieldNames(expr *paginationV1.FilterExpr, fieldName string) {
	if expr == nil {
		return
	}

	for i := len(expr.GetConditions()) - 1; i >= 0; i-- {
		cond := expr.GetConditions()[i]
		if cond.GetField() == fieldName {
			expr.Conditions = append(expr.Conditions[:i], expr.Conditions[i+1:]...)
		}
	}

	for _, subExpr := range expr.GetGroups() {
		clearFilterExprByFieldNames(subExpr, fieldName)
	}
}

func (r *PermissionRepo) List(ctx context.Context, req *paginationV1.PagingRequest, limitPermissionIDs []uint32) (*operatorV1.ListPermissionResponse, error) {
	if req == nil {
		return nil, operatorV1.ErrorPermissionBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().Permission.Query()

	if len(limitPermissionIDs) > 0 {
		filterExpr, err := r.repository.ConvertFilterByPagingRequest(req)
		if err != nil {
			return nil, err
		}
		if filterExpr != nil {
			clearFilterExprByFieldNames(filterExpr, "id")
			req.FilteringType = &paginationV1.PagingRequest_FilterExpr{FilterExpr: filterExpr}
		}

		var strIDs []string
		for _, id := range limitPermissionIDs {
			strIDs = append(strIDs, strconv.FormatUint(uint64(id), 10))
		}

		condition := &paginationV1.FilterCondition{
			Field:  "id",
			Op:     paginationV1.Operator_IN,
			Values: strIDs,
		}

		if req.FilteringType == nil {
			req.FilteringType = &paginationV1.PagingRequest_FilterExpr{
				FilterExpr: &paginationV1.FilterExpr{
					Type: paginationV1.ExprType_AND,
					Conditions: []*paginationV1.FilterCondition{
						condition,
					},
				},
			}
		} else {
			req.GetFilterExpr().Conditions = append(req.GetFilterExpr().GetConditions(), condition)
		}
	}

	ret, err := r.repository.ListWithPaging(ctx, builder, builder.Clone(), req)
	if err != nil {
		return nil, err
	}
	if ret == nil {
		return &operatorV1.ListPermissionResponse{
			Total: 0,
			Items: nil,
		}, nil
	}

	hasMenuID := hasPath("menu_id", req.GetFieldMask())
	hasApiID := hasPath("api_id", req.GetFieldMask())

	for _, dto := range ret.Items {
		if hasMenuID {
			menuIDs, err := r.permissionMenuRepo.ListMenuIDs(ctx, []uint32{dto.GetId()})
			if err != nil {
				return nil, err
			}
			dto.MenuIds = menuIDs
		}
		if hasApiID {
			apiIDs, err := r.permissionApiRepo.ListApiIDs(ctx, []uint32{dto.GetId()})
			if err != nil {
				return nil, err
			}
			dto.ApiIds = apiIDs
		}
	}

	return &operatorV1.ListPermissionResponse{
		Total: ret.Total,
		Items: ret.Items,
	}, nil
}

// GetPermissionCodesByIDs 通过权限ID列表获取权限代码列表
func (r *PermissionRepo) GetPermissionCodesByIDs(ctx context.Context, ids []uint32) ([]string, error) {
	q := r.dbClient.Client().Permission.Query().
		Where(
			permission.IDIn(ids...),
			permission.StatusEQ(permission.StatusOn),
		)

	codes, err := q.
		Select(permission.FieldCode).
		Strings(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query permission codes by ids failed: %s", err.Error())
		return nil, operatorV1.ErrorPermissionInternalServerError("query permission codes by ids failed")
	}
	return codes, nil
}

// GetPermissionIDsByCodes 通过权限代码列表获取权限ID列表
func (r *PermissionRepo) GetPermissionIDsByCodes(ctx context.Context, codes []string) ([]uint32, error) {
	q := r.dbClient.Client().Permission.Query().
		Where(
			permission.CodeIn(codes...),
		)

	intIDs, err := q.
		Select(permission.FieldID).
		Ints(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query permission ids by codes failed: %s", err.Error())
		return nil, operatorV1.ErrorPermissionInternalServerError("query permission ids by codes failed")
	}
	ids := make([]uint32, len(intIDs))
	for i, v := range intIDs {
		ids[i] = uint32(v)
	}
	return ids, nil
}

// GetPermissionIDsByCodesWithTx 通过权限代码列表获取权限ID列表
func (r *PermissionRepo) GetPermissionIDsByCodesWithTx(ctx context.Context, tx *store.Tx, codes []string) ([]uint32, error) {
	q := tx.Permission.Query().
		Where(
			permission.CodeIn(codes...),
		)

	intIDs, err := q.
		Select(permission.FieldID).
		Ints(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query permission ids by codes failed: %s", err.Error())
		return nil, operatorV1.ErrorPermissionInternalServerError("query permission ids by codes failed")
	}
	ids := make([]uint32, len(intIDs))
	for i, v := range intIDs {
		ids[i] = uint32(v)
	}
	return ids, nil
}

// IsExist 检查 Permission 是否存在
func (r *PermissionRepo) IsExist(ctx context.Context, id uint32) (bool, error) {
	exist, err := r.dbClient.Client().Permission.Query().
		Where(permission.IDEQ(id)).
		Exist(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query exist failed: %s", err.Error())
		return false, operatorV1.ErrorPermissionInternalServerError("query exist failed")
	}
	return exist, nil
}

// Get 获取 Permission
func (r *PermissionRepo) Get(ctx context.Context, req *operatorV1.GetPermissionRequest) (*operatorV1.Permission, error) {
	if req == nil {
		return nil, operatorV1.ErrorPermissionBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().Permission.Query()

	var whereCond []queryscope.Scope
	switch req.QueryBy.(type) {
	default:
	case *operatorV1.GetPermissionRequest_Id:
		whereCond = append(whereCond, permission.IDEQ(req.GetId()))
	case *operatorV1.GetPermissionRequest_Code:
		whereCond = append(whereCond, permission.CodeEQ(req.GetCode()))
	}

	dto, err := r.repository.Get(ctx, builder, req.GetViewMask(), whereCond...)
	if err != nil {
		return nil, err
	}

	if hasPath("api_id", req.GetViewMask()) {
		apiIDs, err := r.permissionApiRepo.ListApiIDs(ctx, []uint32{dto.GetId()})
		if err != nil {
			return nil, err
		}
		dto.ApiIds = apiIDs
	}
	if hasPath("menu_id", req.GetViewMask()) {
		menuIDs, err := r.permissionMenuRepo.ListMenuIDs(ctx, []uint32{dto.GetId()})
		if err != nil {
			return nil, err
		}
		dto.MenuIds = menuIDs
	}

	return dto, err
}

func hasPath(path string, fieldMask *fieldmaskpb.FieldMask) bool {
	if fieldMask == nil {
		return true
	}
	for _, p := range fieldMask.GetPaths() {
		if path == p {
			return true
		}
	}
	return false
}

// Create 创建 Permission
func (r *PermissionRepo) Create(ctx context.Context, req *operatorV1.CreatePermissionRequest) error {
	if req == nil || req.Data == nil {
		return operatorV1.ErrorPermissionBadRequest("invalid parameter")
	}

	builder := r.newPermissionCreate(req.Data)

	var entity *store.Permission
	var err error
	if entity, err = builder.Save(ctx); err != nil {
		r.log.Errorf(ctx, "insert permission failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("insert permission failed")
	}

	if len(req.Data.ApiIds) > 0 {
		if err = r.permissionApiRepo.AssignApis(ctx, entity.ID, req.Data.GetApiIds()); err != nil {
			return err
		}
	}
	if len(req.Data.MenuIds) > 0 {
		if err = r.permissionMenuRepo.AssignMenus(ctx, entity.ID, req.Data.GetMenuIds()); err != nil {
			return err
		}
	}

	return nil
}

// BatchCreate 批量创建 Permission
func (r *PermissionRepo) BatchCreate(ctx context.Context, permissions []*operatorV1.Permission) (err error) {
	if len(permissions) == 0 {
		return operatorV1.ErrorPermissionBadRequest("invalid parameter")
	}

	var permissionCreates []*store.PermissionCreate
	for _, perm := range permissions {
		pc := r.newPermissionCreate(perm)
		permissionCreates = append(permissionCreates, pc)
	}

	builder := r.dbClient.Client().Permission.CreateBulk(permissionCreates...)

	var entities []*store.Permission
	if entities, err = builder.Save(ctx); err != nil {
		r.log.Errorf(ctx, "batch insert permissions failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("batch insert permissions failed")
	}

	for i, perm := range permissions {
		entity := entities[i]

		if err = r.permissionApiRepo.AssignApis(ctx, entity.ID, perm.GetApiIds()); err != nil {
			return err
		}

		if err = r.permissionMenuRepo.AssignMenus(ctx, entity.ID, perm.GetMenuIds()); err != nil {
			return err
		}
	}

	return nil
}

// newPermissionCreate 创建 Permission Create 构造器
func (r *PermissionRepo) newPermissionCreate(permission *operatorV1.Permission) *store.PermissionCreate {
	builder := r.dbClient.Client().Permission.Create().
		SetName(permission.GetName()).
		SetCode(permission.GetCode()).
		SetNillableStatus(r.statusConverter.ToEntity(permission.Status)).
		SetNillableDescription(permission.Description).
		SetNillableGroupID(permission.GroupId).
		SetNillableCreatedBy(permission.CreatedBy).
		SetCreatedAt(time.Now())

	if permission.Id != nil {
		builder.SetID(permission.GetId())
	}

	return builder
}

// Update 更新 Permission
func (r *PermissionRepo) Update(ctx context.Context, req *operatorV1.UpdatePermissionRequest) error {
	if req == nil || req.Data == nil {
		return operatorV1.ErrorPermissionBadRequest("invalid parameter")
	}
	if req.GetId() == 0 {
		return operatorV1.ErrorPermissionBadRequest("id is required")
	}

	// 如果不存在则创建
	if req.GetAllowMissing() {
		exist, err := r.IsExist(ctx, req.GetId())
		if err != nil {
			return err
		}
		if !exist {
			createReq := &operatorV1.CreatePermissionRequest{Data: req.Data}
			createReq.Data.CreatedBy = createReq.Data.UpdatedBy
			createReq.Data.UpdatedBy = nil
			return r.Create(ctx, createReq)
		}
	}

	// 关联载荷先快照：UpdateOne 内部的 FilterByFieldMask 会把不在 mask 里的 Data 字段清零，
	// 而 api_ids / menu_ids 恰被下面的黑名单移出 mask——不先取就走不到 Assign（编辑权限会静默
	// 清空它的全部菜单与接口授权）。语义照 role_repo / user_repo 的快照模式：提交了该项（含清空）
	// 才整体替换，没提交则维持原关联。
	updateApis := hasPath("api_ids", req.UpdateMask) || hasPath("apiIds", req.UpdateMask)
	updateMenus := hasPath("menu_ids", req.UpdateMask) || hasPath("menuIds", req.UpdateMask)
	var wantApiIds, wantMenuIds []uint32
	if updateApis {
		wantApiIds = slices.Clone(req.Data.GetApiIds())
	}
	if updateMenus {
		wantMenuIds = slices.Clone(req.Data.GetMenuIds())
	}

	// 剔除关联字段：api_ids / menu_ids 是关联表字段，并非 sys_permissions 表的列。
	// 若保留在 updateMask 中，当其值为空时会被当作 nil 字段生成 SET api_ids=NULL 的 SQL，触发列不存在错误。
	// 关联关系由下方的 AssignApis / AssignMenus 单独维护。
	if req.UpdateMask != nil {
		req.UpdateMask.Paths = sliceutil.FilterBlacklist(req.UpdateMask.GetPaths(), []string{
			"api_ids", "menu_ids",
		})
	}

	builder := r.dbClient.Client().Permission.UpdateOneID(req.GetId())
	perm, err := r.repository.UpdateOne(ctx, builder, req.Data, req.GetUpdateMask(),
		func(dto *operatorV1.Permission) {
			builder.
				SetNillableName(req.Data.Name).
				SetNillableCode(req.Data.Code).
				SetNillableGroupID(req.Data.GroupId).
				SetNillableStatus(r.statusConverter.ToEntity(req.Data.Status)).
				SetNillableDescription(req.Data.Description).
				SetNillableUpdatedBy(req.Data.UpdatedBy).
				SetUpdatedAt(time.Now())
		},
		queryscope.Equal(permission.FieldID, req.GetId()),
	)
	if err != nil {
		return err
	}

	if updateApis {
		if err = r.permissionApiRepo.AssignApis(ctx, perm.GetId(), wantApiIds); err != nil {
			return err
		}
	}

	if updateMenus {
		if err = r.permissionMenuRepo.AssignMenus(ctx, perm.GetId(), wantMenuIds); err != nil {
			return err
		}
	}

	return nil
}

// Delete 删除 Permission
func (r *PermissionRepo) Delete(ctx context.Context, req *operatorV1.DeletePermissionRequest) (err error) {
	if req == nil {
		return operatorV1.ErrorPermissionBadRequest("invalid parameter")
	}

	var permissionIDs []uint32
	switch req.QueryBy.(type) {
	default:
	case *operatorV1.DeletePermissionRequest_Id:
		permissionIDs = append(permissionIDs, req.GetId())

	case *operatorV1.DeletePermissionRequest_Code:
		// 注意：Query.IDs/Ints 内部已追加字段选择，外层再 Select 会重复列导致 scan 失败。
		permissionIDs, err = r.dbClient.Client().Permission.Query().
			Where(permission.CodeEQ(req.GetCode())).
			IDs(ctx)
		if err != nil {
			r.log.Errorf(ctx, "get permission ids by code failed: %s", err.Error())
			return operatorV1.ErrorPermissionInternalServerError("get permission ids by code failed")
		}

	case *operatorV1.DeletePermissionRequest_GroupId:
		permissionIDs, err = r.dbClient.Client().Permission.Query().
			Where(permission.GroupIDEQ(req.GetGroupId())).
			IDs(ctx)
		if err != nil {
			r.log.Errorf(ctx, "get permission ids by group id failed: %s", err.Error())
			return operatorV1.ErrorPermissionInternalServerError("get permission ids by group id failed")
		}
	}

	builder := r.dbClient.Client().Permission.Delete()
	switch req.QueryBy.(type) {
	default:
	case *operatorV1.DeletePermissionRequest_Id:
		builder.Where(permission.IDEQ(req.GetId()))
	case *operatorV1.DeletePermissionRequest_Code:
		builder.Where(permission.CodeEQ(req.GetCode()))
	case *operatorV1.DeletePermissionRequest_GroupId:
		builder.Where(permission.GroupIDEQ(req.GetGroupId()))
	}

	_, err = builder.Exec(ctx)
	if err != nil {
		r.log.Errorf(ctx, "delete permission failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("delete permission failed")
	}

	if err = r.permissionApiRepo.DeleteByPermissionIDs(ctx, permissionIDs); err != nil {
		return err
	}

	if err = r.permissionMenuRepo.DeleteByPermissionIDs(ctx, permissionIDs); err != nil {
		return err
	}

	return nil
}

// Truncate 清空表数据
func (r *PermissionRepo) Truncate(ctx context.Context) error {
	if _, err := r.dbClient.Client().Permission.Delete().Exec(ctx); err != nil {
		r.log.Errorf(ctx, "failed to truncate permission table: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("truncate failed")
	}

	if err := r.permissionApiRepo.Truncate(ctx); err != nil {
		return err
	}

	if err := r.permissionMenuRepo.Truncate(ctx); err != nil {
		return err
	}

	return nil
}

// CleanPermissionsByCodes 清理指定权限代码的权限
func (r *PermissionRepo) CleanPermissionsByCodes(ctx context.Context, codes []string) error {
	builder := r.dbClient.Client().Permission.Delete().
		Where(
			permission.CodeIn(codes...),
		)

	_, err := builder.Exec(ctx)
	if err != nil {
		r.log.Errorf(ctx, "delete permissions by codes failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("delete permissions by codes failed")
	}

	return nil
}

// CleanApiPermissions 清理API相关权限
func (r *PermissionRepo) CleanApiPermissions(ctx context.Context) error {
	return r.permissionApiRepo.Truncate(ctx)
}

// CleanDataPermissions 清理数据权限
func (r *PermissionRepo) CleanDataPermissions(ctx context.Context) error {
	return nil
}

// CleanMenuPermissions 清理菜单相关权限
func (r *PermissionRepo) CleanMenuPermissions(ctx context.Context) error {
	return r.permissionMenuRepo.Truncate(ctx)
}

// TruncateBizPermissions 清理业务权限
func (r *PermissionRepo) TruncateBizPermissions(ctx context.Context) error {
	builder := r.dbClient.Client().Permission.Delete().
		Where(
			permission.Not(permission.CodeHasPrefix(constants.SystemPermissionCodePrefix)),
			permission.Not(permission.CodeHasPrefix("muskdeer:")),
		)

	_, err := builder.Exec(ctx)
	if err != nil {
		r.log.Errorf(ctx, "truncate biz permissions failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("truncate biz permissions failed")
	}

	_ = r.CleanApiPermissions(ctx)
	_ = r.CleanMenuPermissions(ctx)

	return nil
}

// ListApiIDsByPermissionIDs 列出权限关联的API资源ID列表
func (r *PermissionRepo) ListApiIDsByPermissionIDs(ctx context.Context, permissionIDs []uint32) ([]uint32, error) {
	permissionIDs, err := r.activePermissionIDs(ctx, permissionIDs)
	if err != nil || len(permissionIDs) == 0 {
		return nil, err
	}
	apiIDs, err := r.permissionApiRepo.ListApiIDs(ctx, permissionIDs)
	if err != nil {
		return nil, err
	}

	return apiIDs, nil
}

// ListMenuIDsByPermissionIDs 列出权限关联的菜单ID列表
func (r *PermissionRepo) ListMenuIDsByPermissionIDs(ctx context.Context, permissionIDs []uint32) ([]uint32, error) {
	permissionIDs, err := r.activePermissionIDs(ctx, permissionIDs)
	if err != nil || len(permissionIDs) == 0 {
		return nil, err
	}
	apiIDs, err := r.permissionMenuRepo.ListMenuIDs(ctx, permissionIDs)
	if err != nil {
		return nil, err
	}

	return apiIDs, nil
}

func (r *PermissionRepo) activePermissionIDs(ctx context.Context, ids []uint32) ([]uint32, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return r.dbClient.Client().Permission.Query().Where(
		permission.IDIn(ids...),
		permission.StatusEQ(permission.StatusOn),
	).IDs(ctx)
}
