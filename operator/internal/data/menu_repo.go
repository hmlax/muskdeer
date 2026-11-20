package data

import (
	"context"
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
	"github.com/hmlax/muskdeer/operator/internal/data/store/menu"

	"github.com/tx7do/go-utils/trans"
	"google.golang.org/protobuf/proto"

	"github.com/hmlax/muskdeer/deer/management/constants"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

type MenuRepo struct {
	dbClient   *store.DBClient
	driverName string
	log        *bLogger.Helper

	mapper          *mapper.CopierMapper[operatorV1.Menu, store.Menu]
	statusConverter *mapper.EnumTypeConverter[operatorV1.Menu_Status, menu.Status]
	typeConverter   *mapper.EnumTypeConverter[operatorV1.Menu_Type, menu.Type]
	moduleConverter *mapper.EnumTypeConverter[operatorV1.Module_Module, menu.Module]

	repository *store.Repository[operatorV1.Menu, store.Menu]
}

func NewMenuRepo(ctx *bootstrap.Context, dbClient *store.DBClient) *MenuRepo {
	repo := &MenuRepo{
		log:             ctx.NewLoggerHelper("menu/repo/admin-service"),
		dbClient:        dbClient,
		driverName:      driverNameOf(ctx),
		mapper:          mapper.NewCopierMapper[operatorV1.Menu, store.Menu](),
		statusConverter: mapper.NewEnumTypeConverter[operatorV1.Menu_Status, menu.Status](operatorV1.Menu_Status_name, operatorV1.Menu_Status_value),
		typeConverter:   mapper.NewEnumTypeConverter[operatorV1.Menu_Type, menu.Type](operatorV1.Menu_Type_name, operatorV1.Menu_Type_value),
		moduleConverter: mapper.NewEnumTypeConverter[operatorV1.Module_Module, menu.Module](operatorV1.Module_Module_name, operatorV1.Module_Module_value),
	}

	repo.init()

	return repo
}

func (r *MenuRepo) init() {
	r.repository = store.NewRepository[operatorV1.Menu, store.Menu](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())

	r.mapper.AppendConverters(r.statusConverter.NewConverterPair())
	r.mapper.AppendConverters(r.typeConverter.NewConverterPair())
	r.mapper.AppendConverters(r.moduleConverter.NewConverterPair())
}

func (r *MenuRepo) Count(ctx context.Context, whereCond []queryscope.Scope) (int, error) {
	builder := r.dbClient.Client().Menu.Query()
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

func (r *MenuRepo) List(ctx context.Context, req *paginationV1.PagingRequest, treeTravel bool) (*operatorV1.ListMenuResponse, error) {
	if req == nil {
		return nil, operatorV1.ErrorPermissionBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().Menu.Query()

	whereSelectors, _, err := r.repository.BuildListSelectorWithPaging(builder, req)
	if err != nil {
		r.log.Errorf(ctx, "parse list param error [%s]", err.Error())
		return nil, operatorV1.ErrorPermissionBadRequest("invalid query parameter")
	}

	entities, err := builder.All(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query menu list failed: %s", err.Error())
		return nil, operatorV1.ErrorPermissionInternalServerError("query menu list failed")
	}

	// 转换所有实体为 DTO
	dtos := make([]*operatorV1.Menu, 0, len(entities))
	for _, entity := range entities {
		dto := r.mapper.ToDTO(entity)
		dtos = append(dtos, dto)
	}

	// 构建树形结构
	if treeTravel {
		dtos = pagination.BuildTree(
			dtos,
			func(node *operatorV1.Menu) *uint32 { return node.Id },
			func(node *operatorV1.Menu) *uint32 { return node.ParentId },
			func(node *operatorV1.Menu) *[]*operatorV1.Menu { return &node.Children },
		)
	}

	count, err := r.Count(ctx, whereSelectors)
	if err != nil {
		return nil, err
	}

	return &operatorV1.ListMenuResponse{
		Total: uint64(count),
		Items: dtos,
	}, nil
}

func (r *MenuRepo) IsExist(ctx context.Context, id uint32) (bool, error) {
	exist, err := r.dbClient.Client().Menu.Query().
		Where(menu.IDEQ(id)).
		Exist(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query exist failed: %s", err.Error())
		return false, operatorV1.ErrorPermissionInternalServerError("query exist failed")
	}
	return exist, nil
}

func (r *MenuRepo) Get(ctx context.Context, req *operatorV1.GetMenuRequest) (*operatorV1.Menu, error) {
	if req == nil {
		return nil, operatorV1.ErrorPermissionBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().Menu.Query()

	var whereCond []queryscope.Scope
	switch req.QueryBy.(type) {
	default:
	case *operatorV1.GetMenuRequest_Id:
		whereCond = append(whereCond, menu.IDEQ(req.GetId()))
	}

	dto, err := r.repository.Get(ctx, builder, req.GetViewMask(), whereCond...)
	if err != nil {
		return nil, err
	}

	return dto, err
}

func (r *MenuRepo) Create(ctx context.Context, req *operatorV1.CreateMenuRequest) error {
	if req == nil || req.Data == nil {
		return operatorV1.ErrorPermissionBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().Menu.Create().
		SetNillableType(r.typeConverter.ToEntity(req.Data.Type)).
		SetNillablePath(req.Data.Path).
		SetNillableRedirect(req.Data.Redirect).
		SetNillableAlias(req.Data.Alias).
		SetNillableName(req.Data.Name).
		SetNillableComponent(req.Data.Component).
		SetNillableStatus(r.statusConverter.ToEntity(req.Data.Status)).
		SetNillableCreatedBy(req.Data.CreatedBy).
		SetCreatedAt(time.Now())

	// parent_id=0 表示挂根节点；proto optional 会把显式 0 当作"已设置"，
	// SetParentID(0) 指向不存在的行触发自引用外键违约，必须按无父级处理。
	if req.Data.ParentId != nil && *req.Data.ParentId > 0 {
		builder.SetParentID(*req.Data.ParentId)
	}

	// module 为 proto 零值（MODULE_UNSPECIFIED）时跳过：GORM schema 未声明该值，
	// SetNillableModule 会触发 ModuleValidator 失败。未指定即留空，等价不写。
	if req.Data.Module != nil && *req.Data.Module != operatorV1.Module_MODULE_UNSPECIFIED {
		builder.SetNillableModule(r.moduleConverter.ToEntity(req.Data.Module))
	}

	if req.Data.Meta != nil {
		builder.SetMeta(req.Data.Meta)
	}

	if req.Data.Id != nil {
		builder.SetID(req.GetData().GetId())
	}

	if err := builder.Exec(ctx); err != nil {
		r.log.Errorf(ctx, "insert menu failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("insert menu failed")
	}

	return nil
}

// CreateReturn 创建菜单并返回包含数据库生成 ID 的实体
// CreateReturn creates a menu and returns the entity with the database-generated ID
func (r *MenuRepo) CreateReturn(ctx context.Context, req *operatorV1.CreateMenuRequest) (*operatorV1.Menu, error) {
	if req == nil || req.Data == nil {
		return nil, operatorV1.ErrorPermissionBadRequest("invalid parameter")
	}

	return r.createMenuReturn(ctx, r.dbClient.Client(), req.Data)
}

// newMenuCreate 在指定 client（事务或直连）上构建菜单插入 builder
func (r *MenuRepo) newMenuCreate(client *store.Client, data *operatorV1.Menu) *store.MenuCreate {
	builder := client.Menu.Create().
		SetNillableType(r.typeConverter.ToEntity(data.Type)).
		SetNillablePath(data.Path).
		SetNillableRedirect(data.Redirect).
		SetNillableAlias(data.Alias).
		SetNillableName(data.Name).
		SetNillableComponent(data.Component).
		SetNillableStatus(r.statusConverter.ToEntity(data.Status)).
		SetNillableCreatedBy(data.CreatedBy).
		SetCreatedAt(time.Now())

	// parent_id=0 表示挂根节点；proto optional 会把显式 0 当作"已设置"，
	// SetParentID(0) 指向不存在的行触发自引用外键违约，必须按无父级处理。
	if data.ParentId != nil && *data.ParentId > 0 {
		builder.SetParentID(*data.ParentId)
	}

	// module 为 proto 零值（MODULE_UNSPECIFIED）时跳过：GORM schema 未声明该值，
	// SetNillableModule 会触发 ModuleValidator 失败。未指定即留空，等价不写。
	if data.Module != nil && *data.Module != operatorV1.Module_MODULE_UNSPECIFIED {
		builder.SetNillableModule(r.moduleConverter.ToEntity(data.Module))
	}

	if data.Meta != nil {
		builder.SetMeta(data.Meta)
	}

	if data.Id != nil {
		builder.SetID(data.GetId())
	}

	return builder
}

func (r *MenuRepo) createMenuReturn(ctx context.Context, client *store.Client, data *operatorV1.Menu) (*operatorV1.Menu, error) {
	entity, err := r.newMenuCreate(client, data).Save(ctx)
	if err != nil {
		r.log.Errorf(ctx, "insert menu failed: %s", err.Error())
		return nil, operatorV1.ErrorPermissionInternalServerError("insert menu failed")
	}

	return r.mapper.ToDTO(entity), nil
}

func (r *MenuRepo) Update(ctx context.Context, req *operatorV1.UpdateMenuRequest) error {
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
			createReq := &operatorV1.CreateMenuRequest{Data: req.Data}
			createReq.Data.CreatedBy = createReq.Data.UpdatedBy
			createReq.Data.UpdatedBy = nil
			return r.Create(ctx, createReq)
		}
	}

	var metaPaths []string
	if req.UpdateMask != nil {
		for _, v := range req.UpdateMask.GetPaths() {
			if strings.HasPrefix(v, "meta.") {
				metaPaths = append(metaPaths, strings.SplitAfter(v, "meta.")[1])
			}
		}
	}

	builder := r.dbClient.Client().Menu.Update()
	err := r.repository.UpdateX(ctx, builder, req.Data, req.GetUpdateMask(),
		func(dto *operatorV1.Menu) {
			builder.
				SetNillableType(r.typeConverter.ToEntity(req.Data.Type)).
				SetNillablePath(req.Data.Path).
				SetNillableRedirect(req.Data.Redirect).
				SetNillableAlias(req.Data.Alias).
				SetNillableName(req.Data.Name).
				SetNillableComponent(req.Data.Component).
				SetNillableStatus(r.statusConverter.ToEntity(req.Data.Status)).
				SetNillableUpdatedBy(req.Data.UpdatedBy).
				SetUpdatedAt(time.Now())

			// parent_id=0 同 Create：按无父级处理，避免外键违约
			if req.Data.ParentId != nil && *req.Data.ParentId > 0 {
				builder.SetParentID(*req.Data.ParentId)
			}

			// module 为 proto 零值（MODULE_UNSPECIFIED）时跳过：GORM schema 未声明该值，
			// SetNillableModule 会触发 ModuleValidator 失败。未指定即不更新该字段。
			if req.Data.Module != nil && *req.Data.Module != operatorV1.Module_MODULE_UNSPECIFIED {
				builder.SetNillableModule(r.moduleConverter.ToEntity(req.Data.Module))
			}

			if req.Data.Meta != nil {
				r.updateMetaField(builder, req.Data.Meta, metaPaths)
			}
		},
		queryscope.Equal(menu.FieldID, req.GetId()),
	)

	return err
}

func (r *MenuRepo) updateMetaField(builder *store.MenuUpdate, meta *operatorV1.MenuMeta, paths []string) {
	if len(paths) == 0 {
		builder.SetMeta(meta)
		return
	}
	fields := make([]store.JSONField, 0, len(paths))
	for _, path := range paths {
		switch path {
		case "activeIcon", "active_icon":
			fields = append(fields, store.JSONField{
				Name:  "activeIcon",
				Value: meta.ActiveIcon,
			})
		case "activePath", "active_path":
			fields = append(fields, store.JSONField{
				Name:  "activePath",
				Value: meta.ActivePath,
			})
		case "affixTab", "affix_tab":
			fields = append(fields, store.JSONField{
				Name:  "affixTab",
				Value: meta.AffixTab,
			})
		case "affixTabOrder", "affix_tab_order":
			fields = append(fields, store.JSONField{
				Name:  "affixTabOrder",
				Value: meta.AffixTabOrder,
			})
		case "authority":
			fields = append(fields, store.JSONField{
				Name:  "authority",
				Value: meta.Authority,
			})
		case "badge":
			fields = append(fields, store.JSONField{
				Name:  "badge",
				Value: meta.Badge,
			})
		case "badgeType", "badge_type":
			fields = append(fields, store.JSONField{
				Name:  "badgeType",
				Value: meta.BadgeType,
			})
		case "badgeVariants", "badge_variants":
			fields = append(fields, store.JSONField{
				Name:  "badgeVariants",
				Value: meta.BadgeVariants,
			})
		case "hideChildrenInMenu", "hide_children_in_menu":
			fields = append(fields, store.JSONField{
				Name:  "hideChildrenInMenu",
				Value: meta.HideChildrenInMenu,
			})
		case "hideInBreadcrumb", "hide_in_breadcrumb":
			fields = append(fields, store.JSONField{
				Name:  "hideInBreadcrumb",
				Value: meta.HideInBreadcrumb,
			})
		case "hideInMenu", "hide_in_menu":
			fields = append(fields, store.JSONField{
				Name:  "hideInMenu",
				Value: meta.HideInMenu,
			})
		case "hideInTab", "hide_in_tab":
			fields = append(fields, store.JSONField{
				Name:  "hideInTab",
				Value: meta.HideInTab,
			})
		case "icon":
			fields = append(fields, store.JSONField{
				Name:  "icon",
				Value: meta.Icon,
			})
		case "iframeSrc", "iframe_src":
			fields = append(fields, store.JSONField{
				Name:  "iframeSrc",
				Value: meta.IframeSrc,
			})
		case "ignoreAccess", "ignore_access":
			fields = append(fields, store.JSONField{
				Name:  "ignoreAccess",
				Value: meta.IgnoreAccess,
			})
		case "keepAlive", "keep_alive":
			fields = append(fields, store.JSONField{
				Name:  "keepAlive",
				Value: meta.KeepAlive,
			})
		case "link":
			fields = append(fields, store.JSONField{
				Name:  "link",
				Value: meta.Link,
			})
		case "loaded":
			fields = append(fields, store.JSONField{
				Name:  "loaded",
				Value: meta.Loaded,
			})
		case "maxNumOfOpenTab", "max_num_of_open_tab":
			fields = append(fields, store.JSONField{
				Name:  "maxNumOfOpenTab",
				Value: meta.MaxNumOfOpenTab,
			})
		case "menuVisibleWithForbidden", "menu_visible_with_forbidden":
			fields = append(fields, store.JSONField{
				Name:  "menuVisibleWithForbidden",
				Value: meta.MenuVisibleWithForbidden,
			})
		case "openInNewWindow", "open_in_new_window":
			fields = append(fields, store.JSONField{
				Name:  "openInNewWindow",
				Value: meta.OpenInNewWindow,
			})
		case "order":
			fields = append(fields, store.JSONField{
				Name:  "order",
				Value: meta.Order,
			})
		case "title":
			fields = append(fields, store.JSONField{
				Name:  "title",
				Value: meta.Title,
			})
		}
	}
	builder.Modify(store.PatchJSON(menu.FieldMeta, fields...))
}

// Truncate 清空菜单表数据
// Clear all menu data from the table
func (r *MenuRepo) Truncate(ctx context.Context) error {
	if _, err := r.dbClient.Client().Menu.Delete().Exec(ctx); err != nil {
		r.log.Errorf(ctx, "failed to truncate menus table: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("truncate menus failed")
	}
	return nil
}

// SyncMenus 事务化菜单同步，两种模式：
//   - REPLACE（缺省，兼容旧客户端）：清空现有菜单后按传入树重建，菜单 ID 全部变化，角色-菜单授权失效；
//   - MERGE：按全路径匹配，已存在则原位更新（保留 ID 与角色授权），缺失才新增；数据库多出的菜单保留不动。
//     更新不触碰 status：管理端手工停用的菜单不会被同步重新启用。
//
// 整个同步在单个事务内执行，任一步失败整体回滚，不会把菜单表留在半空状态。
func (r *MenuRepo) SyncMenus(ctx context.Context, items []*operatorV1.Menu, mode operatorV1.SyncMenusRequest_Mode, operatorId uint32) (count int, err error) {
	tx, err := r.dbClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return 0, operatorV1.ErrorPermissionInternalServerError("start transaction failed")
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

	client := tx.Client

	if mode == operatorV1.SyncMenusRequest_MERGE {
		existing, mapErr := r.buildMenuFullPathMap(ctx, client)
		if mapErr != nil {
			return 0, mapErr
		}
		return r.syncMergeTree(ctx, client, items, nil, "", existing, operatorId)
	}

	if _, err = client.Menu.Delete().Exec(ctx); err != nil {
		r.log.Errorf(ctx, "truncate menus failed: %s", err.Error())
		return 0, operatorV1.ErrorPermissionInternalServerError("truncate menus failed")
	}
	return r.syncInsertTree(ctx, client, items, nil, operatorId)
}

// syncInsertTree 递归插入菜单树：先插父节点拿 ID，再递归子节点（REPLACE 模式）
func (r *MenuRepo) syncInsertTree(ctx context.Context, client *store.Client, items []*operatorV1.Menu, parentID *uint32, operatorId uint32) (int, error) {
	count := 0
	for _, m := range items {
		if m == nil {
			continue
		}

		dto := proto.Clone(m).(*operatorV1.Menu)
		dto.Children = nil
		dto.Id = nil
		if parentID != nil && *parentID > 0 {
			dto.ParentId = parentID
		} else {
			dto.ParentId = nil
		}
		dto.Module = moduleForComponent(m.GetComponent())
		dto.CreatedBy = trans.Ptr(operatorId)
		dto.UpdatedBy = nil

		created, err := r.createMenuReturn(ctx, client, dto)
		if err != nil {
			return count, err
		}
		count++

		if len(m.Children) > 0 {
			childCount, err := r.syncInsertTree(ctx, client, m.Children, created.Id, operatorId)
			if err != nil {
				return count, err
			}
			count += childCount
		}
	}
	return count, nil
}

// syncMergeTree 增量合并：按全路径匹配，已存在则原位更新（保 ID、不动 status），缺失才新增
func (r *MenuRepo) syncMergeTree(
	ctx context.Context,
	client *store.Client,
	items []*operatorV1.Menu,
	parentID *uint32,
	parentFullPath string,
	existing map[string]*store.Menu,
	operatorId uint32,
) (int, error) {
	count := 0
	for _, m := range items {
		if m == nil {
			continue
		}

		fullPath := joinMenuFullPath(parentFullPath, m.GetPath())
		if fullPath == "" {
			continue
		}

		exist := existing[fullPath]
		var rowID uint32
		if exist != nil {
			builder := client.Menu.Update().
				Where(menu.IDEQ(exist.ID)).
				SetNillableType(r.typeConverter.ToEntity(m.Type)).
				SetNillablePath(m.Path).
				SetNillableRedirect(m.Redirect).
				SetNillableComponent(m.Component).
				SetNillableUpdatedBy(trans.Ptr(operatorId)).
				SetUpdatedAt(time.Now())
			if parentID != nil && *parentID > 0 {
				builder.SetParentID(*parentID)
			} else {
				builder.ClearParentID()
			}
			if module := moduleForComponent(m.GetComponent()); module != nil {
				builder.SetNillableModule(r.moduleConverter.ToEntity(module))
			} else {
				builder.ClearModule()
			}
			if m.Meta != nil {
				builder.SetMeta(m.Meta)
			}
			if err := builder.Exec(ctx); err != nil {
				r.log.Errorf(ctx, "merge update menu failed, path: %s, err: %s", fullPath, err.Error())
				return count, operatorV1.ErrorPermissionInternalServerError("merge update menu failed")
			}
			rowID = exist.ID
		} else {
			dto := proto.Clone(m).(*operatorV1.Menu)
			dto.Children = nil
			dto.Id = nil
			if parentID != nil && *parentID > 0 {
				dto.ParentId = parentID
			} else {
				dto.ParentId = nil
			}
			dto.Module = moduleForComponent(m.GetComponent())
			dto.CreatedBy = trans.Ptr(operatorId)
			dto.UpdatedBy = nil

			created, err := r.createMenuReturn(ctx, client, dto)
			if err != nil {
				return count, err
			}
			rowID = created.GetId()
		}
		count++

		if len(m.Children) > 0 {
			childCount, err := r.syncMergeTree(ctx, client, m.Children, &rowID, fullPath, existing, operatorId)
			if err != nil {
				return count, err
			}
			count += childCount
		}
	}
	return count, nil
}

// joinMenuFullPath 计算菜单全路径：子节点相对路径拼在父全路径后，绝对路径子节点按绝对处理
func joinMenuFullPath(parentFullPath, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return parentFullPath
	}
	if strings.HasPrefix(path, "/") {
		return path
	}
	if parentFullPath == "" || parentFullPath == "/" {
		return "/" + path
	}
	return strings.TrimRight(parentFullPath, "/") + "/" + path
}

// buildMenuFullPathMap 加载现有菜单并建立 全路径 → 实体 映射（MERGE 匹配用）
func (r *MenuRepo) buildMenuFullPathMap(ctx context.Context, client *store.Client) (map[string]*store.Menu, error) {
	rows, err := client.Menu.Query().All(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query existing menus failed: %s", err.Error())
		return nil, operatorV1.ErrorPermissionInternalServerError("query existing menus failed")
	}

	childrenOf := make(map[uint32][]*store.Menu, len(rows))
	var roots []*store.Menu
	for _, row := range rows {
		if row.ParentID == nil || *row.ParentID == 0 {
			roots = append(roots, row)
		} else {
			childrenOf[*row.ParentID] = append(childrenOf[*row.ParentID], row)
		}
	}

	existing := make(map[string]*store.Menu, len(rows))
	visited := make(map[uint32]bool, len(rows))
	var walk func(rows []*store.Menu, parentFullPath string)
	walk = func(rows []*store.Menu, parentFullPath string) {
		for _, row := range rows {
			if visited[row.ID] {
				continue // 脏数据成环防护
			}
			visited[row.ID] = true
			fullPath := joinMenuFullPath(parentFullPath, derefStrP(row.Path))
			existing[fullPath] = row
			walk(childrenOf[row.ID], fullPath)
		}
	}
	walk(roots, "")

	return existing, nil
}

// moduleForComponent 由组件路径归类业务模块；未声明组件或无法归类时返回 nil（等价不写）
func moduleForComponent(component string) *operatorV1.Module_Module {
	if component == "" {
		return nil
	}
	mod := constants.ComponentToModule(component)
	if mod == operatorV1.Module_MODULE_UNSPECIFIED {
		return nil
	}
	return &mod
}

func (r *MenuRepo) Delete(ctx context.Context, req *operatorV1.DeleteMenuRequest) error {
	if req == nil {
		return operatorV1.ErrorPermissionBadRequest("invalid parameter")
	}

	childrenIds, err := store.DescendantIDs[store.Menu](ctx, r.dbClient, req.GetId())
	if err != nil {
		r.log.Errorf(ctx, "query child menus failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("query child menus failed")
	}
	childrenIds = append(childrenIds, req.GetId())

	// r.log.Info(ctx, "menu childrenIds to delete: ", childrenIds)

	var ids []any
	for _, id := range childrenIds {
		ids = append(ids, id)
	}

	builder := r.dbClient.Client().Menu.Delete()

	_, err = r.repository.Delete(ctx, builder, queryscope.In(menu.FieldID, ids...))
	if err != nil {
		r.log.Errorf(ctx, "delete menu failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("delete menu failed")
	}

	return nil
}
