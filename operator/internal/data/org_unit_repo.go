package data

import (
	"context"
	"sort"
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
	"github.com/tx7do/go-utils/timeutil"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/orgunit"
	"github.com/hmlax/muskdeer/operator/internal/data/store/position"
	"github.com/hmlax/muskdeer/operator/internal/data/store/predicate"
)

type OrgUnitRepo struct {
	dbClient *store.DBClient
	log      *bLogger.Helper

	userOrgUnitRepo *UserOrgUnitRepo

	mapper          *mapper.CopierMapper[operatorV1.OrgUnit, store.OrgUnit]
	typeConverter   *mapper.EnumTypeConverter[operatorV1.OrgUnit_Type, orgunit.Type]
	statusConverter *mapper.EnumTypeConverter[operatorV1.OrgUnit_Status, orgunit.Status]

	repository *store.Repository[operatorV1.OrgUnit, store.OrgUnit]
}

func NewOrgUnitRepo(ctx *bootstrap.Context, dbClient *store.DBClient, userOrgUnitRepo *UserOrgUnitRepo) *OrgUnitRepo {
	repo := &OrgUnitRepo{
		log:             ctx.NewLoggerHelper("org-unit/repo/admin-service"),
		dbClient:        dbClient,
		userOrgUnitRepo: userOrgUnitRepo,
		mapper:          mapper.NewCopierMapper[operatorV1.OrgUnit, store.OrgUnit](),
		typeConverter:   mapper.NewEnumTypeConverter[operatorV1.OrgUnit_Type, orgunit.Type](operatorV1.OrgUnit_Type_name, operatorV1.OrgUnit_Type_value),
		statusConverter: mapper.NewEnumTypeConverter[operatorV1.OrgUnit_Status, orgunit.Status](operatorV1.OrgUnit_Status_name, operatorV1.OrgUnit_Status_value),
	}

	repo.init()

	return repo
}

func (r *OrgUnitRepo) init() {
	r.repository = store.NewRepository[operatorV1.OrgUnit, store.OrgUnit](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())

	r.mapper.AppendConverters(r.typeConverter.NewConverterPair())
	r.mapper.AppendConverters(r.statusConverter.NewConverterPair())
}

func (r *OrgUnitRepo) count(ctx context.Context, whereCond []queryscope.Scope) (int, error) {
	builder := r.dbClient.Client().OrgUnit.Query()
	if len(whereCond) != 0 {
		builder.Modify(whereCond...)
	}

	count, err := builder.Count(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query count failed: %s", err.Error())
		return 0, operatorV1.ErrorIdentityInternalServerError("query count failed")
	}

	return count, nil
}

func (r *OrgUnitRepo) Count(ctx context.Context, req *paginationV1.PagingRequest) (int, error) {
	builder := r.dbClient.Client().OrgUnit.Query()

	whereSelectors, _, _ := r.repository.BuildListSelectorWithPaging(builder, req)
	if len(whereSelectors) != 0 {
		builder.Modify(whereSelectors...)
	}

	count, err := builder.Count(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query org-unit count failed: %s", err.Error())
		return 0, operatorV1.ErrorIdentityInternalServerError("query count failed")
	}

	return count, nil
}

func (r *OrgUnitRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListOrgUnitResponse, error) {
	if req == nil {
		return nil, operatorV1.ErrorIdentityBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().OrgUnit.Query()

	whereSelectors, _, err := r.repository.BuildListSelectorWithPaging(builder, req)
	if err != nil {
		r.log.Errorf(ctx, "parse list param error [%s]", err.Error())
		return nil, operatorV1.ErrorIdentityBadRequest("invalid query parameter")
	}

	entities, err := builder.All(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query org unit list failed: %s", err.Error())
		return nil, operatorV1.ErrorIdentityInternalServerError("query org unit list failed")
	}

	sort.SliceStable(entities, func(i, j int) bool {
		var sortI, sortJ uint32
		if entities[i].SortOrder != nil {
			sortI = *entities[i].SortOrder
		}
		if entities[j].SortOrder != nil {
			sortJ = *entities[j].SortOrder
		}
		return sortI < sortJ
	})

	// 转换所有实体为 DTO
	dtos := make([]*operatorV1.OrgUnit, 0, len(entities))
	for _, entity := range entities {
		dto := r.mapper.ToDTO(entity)
		dtos = append(dtos, dto)
	}

	// 构建树形结构
	dtos = pagination.BuildTree(
		dtos,
		func(node *operatorV1.OrgUnit) *uint32 { return node.Id },
		func(node *operatorV1.OrgUnit) *uint32 { return node.ParentId },
		func(node *operatorV1.OrgUnit) *[]*operatorV1.OrgUnit { return &node.Children },
	)

	count, err := r.count(ctx, whereSelectors)
	if err != nil {
		return nil, err
	}

	return &operatorV1.ListOrgUnitResponse{
		Total: uint64(count),
		Items: dtos,
	}, err
}

func (r *OrgUnitRepo) IsExist(ctx context.Context, id uint32) (bool, error) {
	exist, err := r.dbClient.Client().OrgUnit.Query().
		Where(orgunit.IDEQ(id)).
		Exist(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query exist failed: %s", err.Error())
		return false, operatorV1.ErrorIdentityInternalServerError("query exist failed")
	}
	return exist, nil
}

func (r *OrgUnitRepo) Get(ctx context.Context, req *operatorV1.GetOrgUnitRequest) (*operatorV1.OrgUnit, error) {
	if req == nil {
		return nil, operatorV1.ErrorIdentityBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().OrgUnit.Query()

	var whereCond []queryscope.Scope
	switch req.QueryBy.(type) {
	default:
	case *operatorV1.GetOrgUnitRequest_Id:
		whereCond = append(whereCond, orgunit.IDEQ(req.GetId()))
	}

	dto, err := r.repository.Get(ctx, builder, req.GetViewMask(), whereCond...)
	if err != nil {
		return nil, err
	}

	return dto, err
}

// ListOrgUnitsByIds 通过多个ID获取组织列表
func (r *OrgUnitRepo) ListOrgUnitsByIds(ctx context.Context, ids []uint32) ([]*operatorV1.OrgUnit, error) {
	if len(ids) == 0 {
		return []*operatorV1.OrgUnit{}, nil
	}

	entities, err := r.dbClient.Client().OrgUnit.Query().
		Where(orgunit.IDIn(ids...)).
		All(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query orgUnit by ids failed: %s", err.Error())
		return nil, operatorV1.ErrorIdentityInternalServerError("query orgUnit by ids failed")
	}

	dtos := make([]*operatorV1.OrgUnit, 0, len(entities))
	for _, entity := range entities {
		dto := r.mapper.ToDTO(entity)
		dtos = append(dtos, dto)
	}

	return dtos, nil
}

func (r *OrgUnitRepo) Create(ctx context.Context, req *operatorV1.CreateOrgUnitRequest) (err error) {
	if req == nil || req.Data == nil {
		return operatorV1.ErrorIdentityBadRequest("invalid parameter")
	}

	var tx *store.Tx
	tx, err = r.dbClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return operatorV1.ErrorIdentityInternalServerError("start transaction failed")
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
			err = operatorV1.ErrorIdentityInternalServerError("transaction commit failed")
		}
	}()

	builder := tx.OrgUnit.Create().
		SetNillableTenantID(req.Data.TenantId).
		SetName(req.Data.GetName()).
		SetNillableCode(req.Data.Code).
		SetNillableStatus(r.statusConverter.ToEntity(req.Data.Status)).
		SetNillableType(r.typeConverter.ToEntity(req.Data.Type)).
		SetNillablePath(req.Data.Path).
		SetNillableParentID(req.Data.ParentId).
		SetNillableSortOrder(req.Data.SortOrder).
		SetNillableLeaderID(req.Data.LeaderId).
		SetNillableDescription(req.Data.Description).
		SetNillableRemark(req.Data.Remark).
		SetNillableExternalID(req.Data.ExternalId).
		SetNillableIsLegalEntity(req.Data.IsLegalEntity).
		SetNillableRegistrationNumber(req.Data.RegistrationNumber).
		SetNillableTaxID(req.Data.TaxId).
		SetNillableLegalEntityOrgID(req.Data.LegalEntityOrgId).
		SetNillableAddress(req.Data.Address).
		SetNillablePhone(req.Data.Phone).
		SetNillableEmail(req.Data.Email).
		SetNillableTimezone(req.Data.Timezone).
		SetNillableCountry(req.Data.Country).
		SetNillableLatitude(req.Data.Latitude).
		SetNillableLongitude(req.Data.Longitude).
		SetNillableStartAt(timeutil.TimestamppbToTime(req.Data.StartAt)).
		SetNillableEndAt(timeutil.TimestamppbToTime(req.Data.EndAt)).
		SetNillableContactUserID(req.Data.ContactUserId).
		SetNillableCreatedBy(req.Data.CreatedBy).
		SetCreatedAt(time.Now())

	if req.Data.BusinessScopes == nil {
		builder.SetBusinessScopes(req.Data.GetBusinessScopes())
	}
	if req.Data.PermissionTags == nil {
		builder.SetPermissionTags(req.Data.GetPermissionTags())
	}

	if req.Data.Id != nil {
		builder.SetID(req.GetData().GetId())
	}

	var entity *store.OrgUnit
	if entity, err = builder.Save(ctx); err != nil {
		r.log.Errorf(ctx, "insert org unit failed: %s", err.Error())
		return operatorV1.ErrorIdentityInternalServerError("insert org unit failed")
	}

	if err = r.setTreePath(ctx, tx, entity); err != nil {
		return err
	}

	return nil
}

func (r *OrgUnitRepo) Update(ctx context.Context, req *operatorV1.UpdateOrgUnitRequest) error {
	if req == nil || req.Data == nil {
		return operatorV1.ErrorIdentityBadRequest("invalid parameter")
	}
	if req.GetId() == 0 {
		return operatorV1.ErrorIdentityBadRequest("id is required")
	}

	// 如果不存在则创建
	if req.GetAllowMissing() {
		exist, err := r.IsExist(ctx, req.GetId())
		if err != nil {
			return err
		}
		if !exist {
			createReq := &operatorV1.CreateOrgUnitRequest{Data: req.Data}
			createReq.Data.CreatedBy = createReq.Data.UpdatedBy
			createReq.Data.UpdatedBy = nil
			return r.Create(ctx, createReq)
		}
	}

	builder := r.dbClient.Client().OrgUnit.Update()
	err := r.repository.UpdateX(ctx, builder, req.Data, req.GetUpdateMask(),
		func(dto *operatorV1.OrgUnit) {
			builder.
				SetNillableName(req.Data.Name).
				SetNillableCode(req.Data.Code).
				SetNillableStatus(r.statusConverter.ToEntity(req.Data.Status)).
				SetNillableType(r.typeConverter.ToEntity(req.Data.Type)).
				SetNillablePath(req.Data.Path).
				SetNillableParentID(req.Data.ParentId).
				SetNillableSortOrder(req.Data.SortOrder).
				SetNillableLeaderID(req.Data.LeaderId).
				SetNillableDescription(req.Data.Description).
				SetNillableRemark(req.Data.Remark).
				SetNillableExternalID(req.Data.ExternalId).
				SetNillableIsLegalEntity(req.Data.IsLegalEntity).
				SetNillableRegistrationNumber(req.Data.RegistrationNumber).
				SetNillableTaxID(req.Data.TaxId).
				SetNillableLegalEntityOrgID(req.Data.LegalEntityOrgId).
				SetNillableAddress(req.Data.Address).
				SetNillablePhone(req.Data.Phone).
				SetNillableEmail(req.Data.Email).
				SetNillableTimezone(req.Data.Timezone).
				SetNillableCountry(req.Data.Country).
				SetNillableLatitude(req.Data.Latitude).
				SetNillableLongitude(req.Data.Longitude).
				SetNillableStartAt(timeutil.TimestamppbToTime(req.Data.StartAt)).
				SetNillableEndAt(timeutil.TimestamppbToTime(req.Data.EndAt)).
				SetNillableContactUserID(req.Data.ContactUserId).
				SetNillableUpdatedBy(req.Data.UpdatedBy).
				SetUpdatedAt(time.Now())

			if req.Data.BusinessScopes == nil {
				builder.SetBusinessScopes(req.Data.GetBusinessScopes())
			}
			if req.Data.PermissionTags == nil {
				builder.SetPermissionTags(req.Data.GetPermissionTags())
			}
		},
		queryscope.Equal(orgunit.FieldID, req.GetId()),
	)
	if err != nil {
		return err
	}

	// parent_id 变更后重算本节点及全部后代的物化路径（path 前缀是数据范围
	// UNIT_AND_CHILD 展开的依据，挂载关系变了路径必须跟着走）。
	if req.Data.ParentId != nil {
		if err = r.relocateSubtree(ctx, req.GetId()); err != nil {
			return err
		}
	}

	return err
}

// relocateSubtree 以 parent_id 链为准，BFS 重算 node 及其全部后代的物化路径。
// 不按旧 path 前缀扫描：历史数据可能存着脏路径（如全表都是 "/"），按 parent 链
// 走可顺带自愈；移动到自身后代下会成环，检测到即拒绝。
func (r *OrgUnitRepo) relocateSubtree(ctx context.Context, nodeID uint32) error {
	client := r.dbClient.Client()

	node, err := client.OrgUnit.Query().
		Where(orgunit.IDEQ(nodeID)).
		Select(orgunit.FieldPath, orgunit.FieldParentID).
		Only(ctx)
	if err != nil {
		r.log.Errorf(ctx, "relocate subtree: query org unit [%d] failed: %s", nodeID, err.Error())
		return operatorV1.ErrorIdentityInternalServerError("query org unit failed")
	}

	var parentPath string
	if node.ParentID != nil && *node.ParentID != 0 {
		var parent *store.OrgUnit
		parent, err = client.OrgUnit.Query().
			Where(orgunit.IDEQ(*node.ParentID)).
			Select(orgunit.FieldPath).
			Only(ctx)
		if err != nil {
			r.log.Errorf(ctx, "relocate subtree: query parent org unit [%d] failed: %s", *node.ParentID, err.Error())
			return operatorV1.ErrorIdentityInternalServerError("query parent org unit failed")
		}
		if parent.Path != nil {
			parentPath = *parent.Path
		}
		if parentPath != "" && strings.Contains(parentPath, "/"+strconv.FormatUint(uint64(nodeID), 10)+"/") {
			return operatorV1.ErrorIdentityBadRequest("cannot move org unit under its own descendant")
		}
	}

	type queueItem struct {
		id         uint32
		parentPath string
	}
	queue := []queueItem{{
		id:         nodeID,
		parentPath: parentPath,
	}}
	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]

		newPath := r.computeUnitTreePath(it.parentPath, it.id)
		cur, cerr := client.OrgUnit.Query().
			Where(orgunit.IDEQ(it.id)).
			Select(orgunit.FieldPath).
			Only(ctx)
		if cerr != nil {
			r.log.Errorf(ctx, "relocate subtree: query org unit [%d] failed: %s", it.id, cerr.Error())
			return operatorV1.ErrorIdentityInternalServerError("query org unit failed")
		}
		if cur.Path == nil || *cur.Path != newPath {
			if _, uerr := client.OrgUnit.UpdateOneID(it.id).SetPath(newPath).Save(ctx); uerr != nil {
				r.log.Errorf(ctx, "relocate subtree: update org unit [%d] path failed: %s", it.id, uerr.Error())
				return operatorV1.ErrorIdentityInternalServerError("update org unit path failed")
			}
		}

		children, cerr := client.OrgUnit.Query().
			Where(orgunit.ParentIDEQ(it.id)).
			Select(orgunit.FieldID).
			All(ctx)
		if cerr != nil {
			r.log.Errorf(ctx, "relocate subtree: query children of [%d] failed: %s", it.id, cerr.Error())
			return operatorV1.ErrorIdentityInternalServerError("query org unit children failed")
		}
		for _, ch := range children {
			queue = append(queue, queueItem{
				id:         ch.ID,
				parentPath: newPath,
			})
		}
	}

	return nil
}

func (r *OrgUnitRepo) Delete(ctx context.Context, req *operatorV1.DeleteOrgUnitRequest) error {
	if req == nil {
		return operatorV1.ErrorIdentityBadRequest("invalid parameter")
	}

	childrenIds, err := store.DescendantIDs[store.OrgUnit](ctx, r.dbClient, req.GetId())
	if err != nil {
		r.log.Errorf(ctx, "query child orgUnits failed: %s", err.Error())
		return operatorV1.ErrorIdentityInternalServerError("query child orgUnits failed")
	}
	childrenIds = append(childrenIds, req.GetId())

	// r.log.Info(ctx, "orgunits childrenIds to delete: ", childrenIds)

	// 岗位硬性挂在单元上（org_unit_id NOT NULL，且无 DB 外键兜底）：子树内
	// 还有岗位时拒绝删除，避免岗位随级联静默消失或产生悬挂引用。
	// 先删除/转移子树内的岗位，再删单元。
	posCnt, err := r.dbClient.Client().Position.Query().
		Where(position.OrgUnitIDIn(childrenIds...)).
		Count(ctx)
	if err != nil {
		r.log.Errorf(ctx, "count positions under org units failed: %s", err.Error())
		return operatorV1.ErrorIdentityInternalServerError("count positions under org units failed")
	}
	if posCnt > 0 {
		return operatorV1.ErrorIdentityBadRequest("exist %d positions under the org unit subtree, delete or move them first", posCnt)
	}

	var ids []any
	for _, id := range childrenIds {
		ids = append(ids, id)
	}

	var tx *store.Tx
	tx, err = r.dbClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return operatorV1.ErrorIdentityInternalServerError("start transaction failed")
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
			err = operatorV1.ErrorIdentityInternalServerError("transaction commit failed")
		}
	}()

	// 用户↔单元是纯绑定关系：随子树删除清理绑定行，用户本身不受影响
	if err = r.userOrgUnitRepo.CleanRelationsByOrgUnitIDs(ctx, tx, childrenIds); err != nil {
		return err
	}

	builder := tx.OrgUnit.Delete()

	_, err = r.repository.Delete(ctx, builder, queryscope.In(orgunit.FieldID, ids...))
	if err != nil {
		r.log.Errorf(ctx, "delete orgUnit failed: %s", err.Error())
		return operatorV1.ErrorIdentityInternalServerError("delete orgUnit failed")
	}

	return nil
}

// setTreePath 计算并落库节点的物化路径：根节点 "/ID/"，子孙节点 "/父路径/ID/"。
// 不能直接用 store.ComputeTreePath 落库：它对空父路径返回 "/"，会让所有根节点
// 共享同一前缀，数据范围 UNIT_AND_CHILD 的 path 前缀展开会因此误匹配全表。
func (r *OrgUnitRepo) setTreePath(ctx context.Context, tx *store.Tx, entity *store.OrgUnit) (err error) {
	var parentPath string
	if entity.ParentID != nil {
		var parentEntity *store.OrgUnit
		parentEntity, err = tx.OrgUnit.Query().
			Where(
				orgunit.IDEQ(*entity.ParentID),
			).
			Select(orgunit.FieldPath).
			Only(ctx)
		if err != nil {
			return err
		} else {
			if parentEntity.Path != nil {
				parentPath = *parentEntity.Path
			}
		}
	}
	err = tx.OrgUnit.UpdateOneID(entity.ID).
		SetPath(r.computeUnitTreePath(parentPath, entity.ID)).
		Exec(ctx)

	return err
}

// computeUnitTreePath 物化路径：根节点 "/ID/"（各根前缀互不相同），
// 子孙节点沿用库函数的 "/父路径/ID/" 拼接。
func (r *OrgUnitRepo) computeUnitTreePath(parentPath string, nodeID uint32) string {
	if parentPath == "" {
		return "/" + strconv.FormatUint(uint64(nodeID), 10) + "/"
	}
	return store.ComputeTreePath(parentPath, nodeID)
}

// ListOrgUnitIDsInTenant 返回 ids 中属于指定租户的子集（跨租户剔除）。
// 登录聚合上下文为 privacy.Allow（绕过租户隐私层），本方法内的显式租户谓词
// 是该路径上唯一的租户防线。
func (r *OrgUnitRepo) ListOrgUnitIDsInTenant(ctx context.Context, tenantID uint32, ids []uint32) ([]uint32, error) {
	if len(ids) == 0 {
		return []uint32{}, nil
	}

	intIDs, err := r.dbClient.Client().OrgUnit.Query().
		Where(
			orgunit.IDIn(ids...),
			orgunit.TenantIDEQ(tenantID),
		).
		IDs(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query org unit ids in tenant failed: %s", err.Error())
		return nil, operatorV1.ErrorIdentityInternalServerError("query org unit ids in tenant failed")
	}

	result := make([]uint32, len(intIDs))
	for i, v := range intIDs {
		result[i] = uint32(v)
	}
	return result, nil
}

// ListSelfAndDescendantOrgUnitIds 返回种子单元集及其全部后代的并集
// （含种子自身），以 path 前缀匹配展开，全程限定指定租户。
// UNIT_AND_CHILD 数据范围的登录期展开专用；登录上下文为 privacy.Allow，
// 显式租户谓词是该路径上唯一的租户防线。
func (r *OrgUnitRepo) ListSelfAndDescendantOrgUnitIds(ctx context.Context, tenantID uint32, seeds []uint32) ([]uint32, error) {
	if len(seeds) == 0 {
		return []uint32{}, nil
	}

	seedPaths, err := r.dbClient.Client().OrgUnit.Query().
		Where(
			orgunit.IDIn(seeds...),
			orgunit.TenantIDEQ(tenantID),
		).
		Select(orgunit.FieldPath).
		Strings(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query seed org unit paths failed: %s", err.Error())
		return nil, operatorV1.ErrorIdentityInternalServerError("query seed org unit paths failed")
	}

	var pathPreds []predicate.OrgUnit
	for _, p := range seedPaths {
		if p == "" {
			continue
		}
		pathPreds = append(pathPreds, orgunit.PathHasPrefix(p))
	}
	if len(pathPreds) == 0 {
		return []uint32{}, nil
	}

	intIDs, err := r.dbClient.Client().OrgUnit.Query().
		Where(
			orgunit.TenantIDEQ(tenantID),
			orgunit.Or(pathPreds...),
		).
		IDs(ctx)
	if err != nil {
		r.log.Errorf(ctx, "expand org unit descendants failed: %s", err.Error())
		return nil, operatorV1.ErrorIdentityInternalServerError("expand org unit descendants failed")
	}

	result := make([]uint32, len(intIDs))
	for i, v := range intIDs {
		result[i] = uint32(v)
	}
	return result, nil
}
