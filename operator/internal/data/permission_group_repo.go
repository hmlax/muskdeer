package data

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/hmlax/muskdeer/operator/internal/data/store/queryscope"

	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	"github.com/tx7do/go-crud/pagination"

	"github.com/tx7do/go-utils/copierutil"
	"github.com/tx7do/go-utils/mapper"

	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/permission"
	"github.com/hmlax/muskdeer/operator/internal/data/store/permissiongroup"

	"github.com/hmlax/muskdeer/deer/management/constants"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

type PermissionGroupRepo struct {
	dbClient *store.DBClient
	log      *bLogger.Helper

	mapper          *mapper.CopierMapper[operatorV1.PermissionGroup, store.PermissionGroup]
	statusConverter *mapper.EnumTypeConverter[operatorV1.PermissionGroup_Status, permissiongroup.Status]

	repository *store.Repository[operatorV1.PermissionGroup, store.PermissionGroup]
}

func NewPermissionGroupRepo(
	ctx *bootstrap.Context,
	dbClient *store.DBClient,
) *PermissionGroupRepo {
	repo := &PermissionGroupRepo{
		log:      ctx.NewLoggerHelper("permission-group/repo/admin-service"),
		dbClient: dbClient,
		mapper:   mapper.NewCopierMapper[operatorV1.PermissionGroup, store.PermissionGroup](),
		statusConverter: mapper.NewEnumTypeConverter[operatorV1.PermissionGroup_Status, permissiongroup.Status](
			operatorV1.PermissionGroup_Status_name, operatorV1.PermissionGroup_Status_value,
		),
	}

	repo.init()

	return repo
}

func (r *PermissionGroupRepo) init() {
	r.repository = store.NewRepository[operatorV1.PermissionGroup, store.PermissionGroup](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())

	r.mapper.AppendConverters(r.statusConverter.NewConverterPair())
}

func (r *PermissionGroupRepo) Count(ctx context.Context, whereCond []queryscope.Scope) (int, error) {
	builder := r.dbClient.Client().PermissionGroup.Query()
	if len(whereCond) != 0 {
		builder.Modify(whereCond...)
	}

	count, err := builder.Count(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query count failed: %s", err.Error())
		return 0, operatorV1.ErrorPermissionInternalServerError("query count failed")
	}

	return count, nil
}

func (r *PermissionGroupRepo) List(ctx context.Context, req *paginationV1.PagingRequest, treeTravel bool) (*operatorV1.ListPermissionGroupResponse, error) {
	if req == nil {
		return nil, operatorV1.ErrorPermissionBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().PermissionGroup.Query()

	whereSelectors, _, err := r.repository.BuildListSelectorWithPaging(builder, req)
	if err != nil {
		r.log.Errorf(ctx, "parse list param error [%s]", err.Error())
		return nil, operatorV1.ErrorPermissionBadRequest("invalid query parameter")
	}

	entities, err := builder.All(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query permission group list failed: %s", err.Error())
		return nil, operatorV1.ErrorPermissionInternalServerError("query permission group list failed")
	}

	// 转换所有实体为 DTO
	dtos := make([]*operatorV1.PermissionGroup, 0, len(entities))
	for _, entity := range entities {
		dto := r.mapper.ToDTO(entity)
		dtos = append(dtos, dto)
	}

	// 构建树形结构
	if treeTravel {
		dtos = pagination.BuildTree(
			dtos,
			func(node *operatorV1.PermissionGroup) *uint32 { return node.Id },
			func(node *operatorV1.PermissionGroup) *uint32 { return node.ParentId },
			func(node *operatorV1.PermissionGroup) *[]*operatorV1.PermissionGroup { return &node.Children },
		)
	}

	count, err := r.Count(ctx, whereSelectors)
	if err != nil {
		return nil, err
	}

	return &operatorV1.ListPermissionGroupResponse{
		Total: uint64(count),
		Items: dtos,
	}, nil
}

func (r *PermissionGroupRepo) IsExist(ctx context.Context, id uint32) (bool, error) {
	exist, err := r.dbClient.Client().PermissionGroup.Query().
		Where(permissiongroup.IDEQ(id)).
		Exist(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query exist failed: %s", err.Error())
		return false, operatorV1.ErrorPermissionInternalServerError("query exist failed")
	}
	return exist, nil
}

func (r *PermissionGroupRepo) Get(ctx context.Context, req *operatorV1.GetPermissionGroupRequest) (*operatorV1.PermissionGroup, error) {
	if req == nil {
		return nil, operatorV1.ErrorPermissionBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().PermissionGroup.Query()

	var whereCond []queryscope.Scope
	switch req.QueryBy.(type) {
	default:
	case *operatorV1.GetPermissionGroupRequest_Id:
		whereCond = append(whereCond, permissiongroup.IDEQ(req.GetId()))
	}

	dto, err := r.repository.Get(ctx, builder, req.GetViewMask(), whereCond...)
	if err != nil {
		return nil, err
	}

	return dto, err
}

// Create 创建 Permission
func (r *PermissionGroupRepo) Create(ctx context.Context, req *operatorV1.CreatePermissionGroupRequest) (dto *operatorV1.PermissionGroup, err error) {
	if req == nil || req.Data == nil {
		return nil, operatorV1.ErrorPermissionBadRequest("invalid parameter")
	}

	var tx *store.Tx
	tx, err = r.dbClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return nil, operatorV1.ErrorPermissionInternalServerError("start transaction failed")
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

	builder := tx.PermissionGroup.Create()
	builder = r.newPermissionCreateWithBuilder(builder, req.Data)

	var entity *store.PermissionGroup
	if entity, err = builder.Save(ctx); err != nil {
		r.log.Errorf(ctx, "insert permission group failed: %s", err.Error())
		return nil, operatorV1.ErrorPermissionInternalServerError("insert permission group failed")
	}

	if err = r.setTreePath(ctx, tx, entity); err != nil {
		return nil, err
	}

	dto = r.mapper.ToDTO(entity)

	return dto, nil
}

// BatchCreate 批量创建 Permission
func (r *PermissionGroupRepo) BatchCreate(ctx context.Context, permissionGroups []*operatorV1.PermissionGroup) (dtos []*operatorV1.PermissionGroup, err error) {
	if len(permissionGroups) == 0 {
		return nil, operatorV1.ErrorPermissionBadRequest("invalid parameter")
	}

	var permissionGroupCreates []*store.PermissionGroupCreate
	for _, perm := range permissionGroups {
		pc := r.newPermissionCreate(perm)
		permissionGroupCreates = append(permissionGroupCreates, pc)
	}

	builder := r.dbClient.Client().PermissionGroup.CreateBulk(permissionGroupCreates...)

	var entities []*store.PermissionGroup
	if entities, err = builder.Save(ctx); err != nil {
		r.log.Errorf(ctx, "batch insert permission groups failed: %s", err.Error())
		return nil, operatorV1.ErrorPermissionInternalServerError("batch insert permission groups failed")
	}

	// 批量插入不经过 setTreePath，路径为 NULL：从批内根（父不在批内的节点）
	// 逐棵重算，BFS 沿 parent_id 自动覆盖同批的父子链。
	inBatch := make(map[uint32]bool, len(entities))
	for _, entity := range entities {
		inBatch[entity.ID] = true
	}
	for _, entity := range entities {
		if entity.ParentID != nil && *entity.ParentID != 0 && inBatch[*entity.ParentID] {
			continue // 批内子节点由其批内父的 relocate 覆盖
		}
		if err = r.relocateSubtree(ctx, r.dbClient.Client(), entity.ID); err != nil {
			return nil, err
		}
	}

	for _, entity := range entities {
		dto := r.mapper.ToDTO(entity)
		dtos = append(dtos, dto)
	}

	return dtos, nil
}

// newPermissionCreate 创建 Permission Create 构造器
func (r *PermissionGroupRepo) newPermissionCreate(permissionGroup *operatorV1.PermissionGroup) *store.PermissionGroupCreate {
	return r.newPermissionCreateWithBuilder(r.dbClient.Client().PermissionGroup.Create(), permissionGroup)
}

func (r *PermissionGroupRepo) newPermissionCreateWithBuilder(builder *store.PermissionGroupCreate, permissionGroup *operatorV1.PermissionGroup) *store.PermissionGroupCreate {
	builder.
		SetName(permissionGroup.GetName()).
		SetNillableStatus(r.statusConverter.ToEntity(permissionGroup.Status)).
		SetNillableModule(permissionGroup.Module).
		SetNillableSortOrder(permissionGroup.SortOrder).
		SetNillableDescription(permissionGroup.Description).
		SetNillableParentID(permissionGroup.ParentId).
		SetNillableCreatedBy(permissionGroup.CreatedBy).
		SetCreatedAt(time.Now())

	if permissionGroup.Id != nil {
		builder.SetID(permissionGroup.GetId())
	}

	return builder
}

// Update 更新 Permission
func (r *PermissionGroupRepo) Update(ctx context.Context, req *operatorV1.UpdatePermissionGroupRequest) error {
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
			createReq := &operatorV1.CreatePermissionGroupRequest{Data: req.Data}
			createReq.Data.CreatedBy = createReq.Data.UpdatedBy
			createReq.Data.UpdatedBy = nil
			_, err = r.Create(ctx, createReq)
			return err
		}
	}

	builder := r.dbClient.Client().PermissionGroup.UpdateOneID(req.GetId())
	_, err := r.repository.UpdateOne(ctx, builder, req.Data, req.GetUpdateMask(),
		func(dto *operatorV1.PermissionGroup) {
			builder.
				SetNillableName(req.Data.Name).
				SetNillableStatus(r.statusConverter.ToEntity(req.Data.Status)).
				SetNillableModule(req.Data.Module).
				SetNillableSortOrder(req.Data.SortOrder).
				SetNillableDescription(req.Data.Description).
				SetNillableParentID(req.Data.ParentId).
				SetNillableUpdatedBy(req.Data.UpdatedBy).
				SetUpdatedAt(time.Now())
		},
		queryscope.Equal(permissiongroup.FieldID, req.GetId()),
	)
	if err != nil {
		return err
	}

	// parent_id 变更后重算本节点及全部后代的物化路径（path 是树形结构的
	// 权威物化形态，挂载关系变了路径必须跟着走）。
	if req.Data.ParentId != nil {
		return r.relocateSubtree(ctx, r.dbClient.Client(), req.GetId())
	}

	return nil
}

// UpdateParentIDs 更新 Permission ParentID
func (r *PermissionGroupRepo) UpdateParentIDs(ctx context.Context, parentIDs map[uint32]uint32) (err error) {
	if len(parentIDs) == 0 {
		return nil
	}

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

	for permID, parentID := range parentIDs {
		builder := tx.PermissionGroup.Update().
			SetParentID(parentID).
			Where(permissiongroup.IDEQ(permID))

		if err = builder.Exec(ctx); err != nil {
			r.log.Errorf(ctx, "update permission parent_id failed: %s", err.Error())
			return operatorV1.ErrorPermissionInternalServerError("update permission parent_id failed")
		}
	}

	// 提交前重算受影响节点及其子树的物化路径（re-parent 后旧路径全部失效）。
	// 必须走 tx.Client：relocate 要读到本事务内未提交的新 parent_id。
	for permID := range parentIDs {
		if err = r.relocateSubtree(ctx, tx.Client, permID); err != nil {
			return err
		}
	}

	return nil
}

// Delete 删除 Permission
func (r *PermissionGroupRepo) Delete(ctx context.Context, req *operatorV1.DeletePermissionGroupRequest) error {
	if req == nil {
		return operatorV1.ErrorPermissionBadRequest("invalid parameter")
	}

	// 有子分组时拒绝：DB 对直接子级是 OnDelete SetNull，不拦会把子分组
	// 静默提升为根，且其 path 残留已删节点的幽灵段。先删子分组再删本组。
	childCnt, err := r.dbClient.Client().PermissionGroup.Query().
		Where(permissiongroup.ParentIDEQ(req.GetId())).
		Count(ctx)
	if err != nil {
		r.log.Errorf(ctx, "count child permission groups failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("count child permission groups failed")
	}
	if childCnt > 0 {
		return operatorV1.ErrorPermissionBadRequest("child permission groups exist, delete them first")
	}

	// 分组下还有权限点时拒绝（group_id 可空，静默置空会丢权限点的分组归属）。
	// 先把权限点转移或删除，再删分组。
	permCnt, err := r.dbClient.Client().Permission.Query().
		Where(permission.GroupIDEQ(req.GetId())).
		Count(ctx)
	if err != nil {
		r.log.Errorf(ctx, "count permissions in permission group failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("count permissions in permission group failed")
	}
	if permCnt > 0 {
		return operatorV1.ErrorPermissionBadRequest("permission points exist in this group, move or delete them first")
	}

	builder := r.dbClient.Client().PermissionGroup.Delete()

	_, err = r.repository.Delete(ctx, builder, queryscope.Equal(permissiongroup.FieldID, req.GetId()))
	if err != nil {
		r.log.Errorf(ctx, "delete permission group failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("delete permission group failed")
	}

	return nil
}

// Truncate 清空表数据
func (r *PermissionGroupRepo) Truncate(ctx context.Context) error {
	if _, err := r.dbClient.Client().PermissionGroup.Delete().Exec(ctx); err != nil {
		r.log.Errorf(ctx, "failed to truncate permission group table: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("truncate failed")
	}

	return nil
}

// TruncateBizGroup 清空业务表数据，保留系统内置数据
func (r *PermissionGroupRepo) TruncateBizGroup(ctx context.Context) error {
	builder := r.dbClient.Client().PermissionGroup.Delete().
		Where(
			permissiongroup.ModuleNotIn(constants.SystemPermissionModule, "muskdeer"),
		)

	if _, err := builder.Exec(ctx); err != nil {
		r.log.Errorf(ctx, "failed to truncate permission group table: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("truncate failed")
	}

	return nil
}

func (r *PermissionGroupRepo) ListByIDs(ctx context.Context, ids []uint32) ([]*operatorV1.PermissionGroup, error) {
	if len(ids) == 0 {
		return []*operatorV1.PermissionGroup{}, nil
	}

	builder := r.dbClient.Client().PermissionGroup.Query().
		Where(permissiongroup.IDIn(ids...))

	entities, err := builder.All(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query list by ids failed: %s", err.Error())
		return nil, operatorV1.ErrorPermissionInternalServerError("query list by ids failed")
	}

	dtos := make([]*operatorV1.PermissionGroup, 0, len(entities))
	for _, entity := range entities {
		dto := r.mapper.ToDTO(entity)
		dtos = append(dtos, dto)
	}

	return dtos, nil
}

func (r *PermissionGroupRepo) setTreePath(ctx context.Context, tx *store.Tx, entity *store.PermissionGroup) (err error) {
	var parentPath string
	if entity.ParentID != nil {
		var parentEntity *store.PermissionGroup
		parentEntity, err = tx.PermissionGroup.Query().
			Where(
				permissiongroup.IDEQ(*entity.ParentID),
			).
			Select(permissiongroup.FieldPath).
			Only(ctx)
		if err != nil {
			return err
		} else {
			if parentEntity.Path != nil {
				parentPath = *parentEntity.Path
			}
		}
	}
	err = tx.PermissionGroup.UpdateOneID(entity.ID).
		SetPath(r.computeGroupTreePath(parentPath, entity.ID)).
		Exec(ctx)

	return err
}

// computeGroupTreePath 物化路径：根节点 "/ID/"（各根前缀互不相同），子孙节点
// "/父路径/ID/"，与 proto 注释的 "/1/10/101/（包含自身）" 格式一致。
// 不能直接用 store.ComputeTreePath：它对空父路径返回 "/"，会让所有根节点
// 共享同一前缀（org_unit 同款问题）。
func (r *PermissionGroupRepo) computeGroupTreePath(parentPath string, nodeID uint32) string {
	if parentPath == "" {
		return "/" + strconv.FormatUint(uint64(nodeID), 10) + "/"
	}
	return store.ComputeTreePath(parentPath, nodeID)
}

// relocateSubtree 以 parent_id 链为准，BFS 重算 node 及其全部后代的物化路径。
// 不按旧 path 前缀扫描：历史/同步写入的数据可能存着脏路径（"/" 或 NULL），
// 按 parent 链走可顺带自愈；移动到自身后代下会成环，检测到即拒绝。
// client 由调用方给：事务内（如 UpdateParentIDs 的 tx.Client）传事务客户端，
// 保证读到同事务内未提交的 parent 变更。
func (r *PermissionGroupRepo) relocateSubtree(ctx context.Context, client *store.Client, nodeID uint32) error {
	node, err := client.PermissionGroup.Query().
		Where(permissiongroup.IDEQ(nodeID)).
		Select(permissiongroup.FieldPath, permissiongroup.FieldParentID).
		Only(ctx)
	if err != nil {
		r.log.Errorf(ctx, "relocate subtree: query permission group [%d] failed: %s", nodeID, err.Error())
		return operatorV1.ErrorPermissionInternalServerError("query permission group failed")
	}

	var parentPath string
	if node.ParentID != nil && *node.ParentID != 0 {
		var parent *store.PermissionGroup
		parent, err = client.PermissionGroup.Query().
			Where(permissiongroup.IDEQ(*node.ParentID)).
			Select(permissiongroup.FieldPath).
			Only(ctx)
		if err != nil {
			r.log.Errorf(ctx, "relocate subtree: query parent permission group [%d] failed: %s", *node.ParentID, err.Error())
			return operatorV1.ErrorPermissionInternalServerError("query parent permission group failed")
		}
		if parent.Path != nil {
			parentPath = *parent.Path
		}
		if parentPath != "" && strings.Contains(parentPath, "/"+strconv.FormatUint(uint64(nodeID), 10)+"/") {
			return operatorV1.ErrorPermissionBadRequest("cannot move permission group under its own descendant")
		}
	}

	type queueItem struct {
		id         uint32
		parentPath string
	}
	visited := map[uint32]bool{nodeID: true}
	queue := []queueItem{{
		id:         nodeID,
		parentPath: parentPath,
	}}
	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]

		newPath := r.computeGroupTreePath(it.parentPath, it.id)
		cur, cerr := client.PermissionGroup.Query().
			Where(permissiongroup.IDEQ(it.id)).
			Select(permissiongroup.FieldPath).
			Only(ctx)
		if cerr != nil {
			r.log.Errorf(ctx, "relocate subtree: query permission group [%d] failed: %s", it.id, cerr.Error())
			return operatorV1.ErrorPermissionInternalServerError("query permission group failed")
		}
		if cur.Path == nil || *cur.Path != newPath {
			if _, uerr := client.PermissionGroup.UpdateOneID(it.id).SetPath(newPath).Save(ctx); uerr != nil {
				r.log.Errorf(ctx, "relocate subtree: update permission group [%d] path failed: %s", it.id, uerr.Error())
				return operatorV1.ErrorPermissionInternalServerError("update permission group path failed")
			}
		}

		children, cerr := client.PermissionGroup.Query().
			Where(permissiongroup.ParentIDEQ(it.id)).
			Select(permissiongroup.FieldID).
			All(ctx)
		if cerr != nil {
			r.log.Errorf(ctx, "relocate subtree: query children of [%d] failed: %s", it.id, cerr.Error())
			return operatorV1.ErrorPermissionInternalServerError("query permission group children failed")
		}
		for _, ch := range children {
			if visited[ch.ID] {
				continue
			}
			visited[ch.ID] = true
			queue = append(queue, queueItem{
				id:         ch.ID,
				parentPath: newPath,
			})
		}
	}

	return nil
}
