package data

import (
	"context"

	"time"

	"github.com/hmlax/muskdeer/operator/internal/data/store/queryscope"

	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"

	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/api"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"

	"github.com/tx7do/go-utils/copierutil"
	"github.com/tx7do/go-utils/mapper"
)

type ApiRepo struct {
	dbClient   *store.DBClient
	driverName string
	log        *bLogger.Helper

	mapper                  *mapper.CopierMapper[operatorV1.Api, store.Api]
	statusConverter         *mapper.EnumTypeConverter[operatorV1.Api_Status, api.Status]
	scopeConverter          *mapper.EnumTypeConverter[operatorV1.Api_Scope, api.Scope]
	businessModuleConverter *mapper.EnumTypeConverter[operatorV1.Module_Module, api.BusinessModule]

	repository *store.Repository[operatorV1.Api, store.Api]
}

func NewApiRepo(ctx *bootstrap.Context, dbClient *store.DBClient) *ApiRepo {
	repo := &ApiRepo{
		log:        ctx.NewLoggerHelper("api/repo/admin-service"),
		dbClient:   dbClient,
		driverName: driverNameOf(ctx),
		mapper:     mapper.NewCopierMapper[operatorV1.Api, store.Api](),
		statusConverter: mapper.NewEnumTypeConverter[operatorV1.Api_Status, api.Status](
			operatorV1.Api_Status_name, operatorV1.Api_Status_value,
		),
		scopeConverter: mapper.NewEnumTypeConverter[operatorV1.Api_Scope, api.Scope](
			operatorV1.Api_Scope_name, operatorV1.Api_Scope_value,
		),
		businessModuleConverter: mapper.NewEnumTypeConverter[operatorV1.Module_Module, api.BusinessModule](
			operatorV1.Module_Module_name, operatorV1.Module_Module_value,
		),
	}

	repo.init()

	return repo
}

func (r *ApiRepo) init() {
	r.repository = store.NewRepository[operatorV1.Api, store.Api](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())

	// status 转换器补注册：本仓 status 实体列为可空指针枚举（SwitchStatus
	// mixin、列默认 ON），但本仓此前从未注册 statusConverter——scope/
	// business_module 均有注册而 status 独缺。mapper 经转换对（指针↔指针）
	// 查表转换，缺注册即无转换对、copier 对该字段直接跳过，读视图恒零值
	// （与 scope 域同表同列形态却长期不可见）。补注册后读路径经 mapper 如实
	// 流通；写路径维持"未指定即走列默认"现状不变。
	r.mapper.AppendConverters(r.statusConverter.NewConverterPair())
	r.mapper.AppendConverters(r.scopeConverter.NewConverterPair())
	r.mapper.AppendConverters(r.businessModuleConverter.NewConverterPair())
}

func (r *ApiRepo) Count(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.CountApiResponse, error) {
	builder := r.dbClient.Client().Api.Query()

	whereSelectors, _, _ := r.repository.BuildListSelectorWithPaging(builder, req)
	if len(whereSelectors) != 0 {
		builder.Modify(whereSelectors...)
	}

	count, err := builder.Count(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query api count failed: %s", err.Error())
		return nil, operatorV1.ErrorPermissionInternalServerError("query api count failed")
	}

	return &operatorV1.CountApiResponse{
		Count: uint64(count),
	}, nil
}

func (r *ApiRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListApiResponse, error) {
	if req == nil {
		return nil, operatorV1.ErrorPermissionBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().Api.Query()

	ret, err := r.repository.ListWithPaging(ctx, builder, builder.Clone(), req)
	if err != nil {
		return nil, err
	}
	if ret == nil {
		return &operatorV1.ListApiResponse{
			Total: 0,
			Items: nil,
		}, nil
	}

	return &operatorV1.ListApiResponse{
		Total: ret.Total,
		Items: ret.Items,
	}, nil
}

func (r *ApiRepo) IsExist(ctx context.Context, id uint32) (bool, error) {
	exist, err := r.dbClient.Client().Api.Query().
		Where(api.IDEQ(id)).
		Exist(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query exist failed: %s", err.Error())
		return false, operatorV1.ErrorPermissionInternalServerError("query exist failed")
	}
	return exist, nil
}

func (r *ApiRepo) Get(ctx context.Context, req *operatorV1.GetApiRequest) (*operatorV1.Api, error) {
	if req == nil {
		return nil, operatorV1.ErrorPermissionBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().Api.Query()

	var whereCond []queryscope.Scope
	switch req.QueryBy.(type) {
	default:
	case *operatorV1.GetApiRequest_Id:
		whereCond = append(whereCond, api.IDEQ(req.GetId()))
	}

	dto, err := r.repository.Get(ctx, builder, req.GetViewMask(), whereCond...)
	if err != nil {
		return nil, err
	}

	return dto, err
}

// GetApiByEndpoint 根据路径和方法获取API资源
func (r *ApiRepo) GetApiByEndpoint(ctx context.Context, path, method string) (*operatorV1.Api, error) {
	if path == "" || method == "" {
		return nil, operatorV1.ErrorPermissionBadRequest("invalid parameter")
	}

	entity, err := r.dbClient.Client().Api.Query().
		Where(
			api.PathEQ(path),
			api.MethodEQ(method),
		).
		Only(ctx)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, operatorV1.ErrorPermissionNotFound("api not found")
		}

		r.log.Errorf(ctx, "query one data failed: %s", err.Error())

		return nil, operatorV1.ErrorPermissionInternalServerError("query data failed")
	}

	return r.mapper.ToDTO(entity), nil
}

// GetApiByIDs 根据ID列表获取API资源
func (r *ApiRepo) GetApiByIDs(ctx context.Context, ids []uint32) ([]*operatorV1.Api, error) {
	if len(ids) == 0 {
		return nil, operatorV1.ErrorPermissionBadRequest("invalid parameter")
	}

	entities, err := r.dbClient.Client().Api.Query().
		Where(
			api.IDIn(ids...),
		).
		All(ctx)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, operatorV1.ErrorPermissionNotFound("api not found")
		}

		r.log.Errorf(ctx, "query one data failed: %s", err.Error())

		return nil, operatorV1.ErrorPermissionInternalServerError("query data failed")
	}

	dtos := make([]*operatorV1.Api, 0, len(entities))
	for _, entity := range entities {
		dto := r.mapper.ToDTO(entity)
		dtos = append(dtos, dto)
	}

	return dtos, nil
}

func (r *ApiRepo) Create(ctx context.Context, req *operatorV1.CreateApiRequest) error {
	if req == nil || req.Data == nil {
		return operatorV1.ErrorPermissionBadRequest("invalid parameter")
	}

	builder := r.newApiCreate(req.Data)

	if err := builder.Exec(ctx); err != nil {
		r.log.Errorf(ctx, "insert api failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("insert api failed")
	}

	return nil
}

func (r *ApiRepo) newApiCreate(api *operatorV1.Api) *store.ApiCreate {
	builder := r.dbClient.Client().Api.Create().
		SetNillableDescription(api.Description).
		SetNillableModule(api.Module).
		SetNillableModuleDescription(api.ModuleDescription).
		SetNillableOperation(api.Operation).
		SetNillablePath(api.Path).
		SetNillableMethod(api.Method).
		SetNillableScope(r.scopeConverter.ToEntity(api.Scope)).
		SetNillableCreatedBy(api.CreatedBy).
		SetCreatedAt(time.Now())

	// business_module 为 proto 零值（MODULE_UNSPECIFIED）时跳过：GORM schema 未声明该值，
	// SetNillableBusinessModule 会触发 BusinessModuleValidator 失败。未指定即留空。
	if api.BusinessModule != nil && *api.BusinessModule != operatorV1.Module_MODULE_UNSPECIFIED {
		builder.SetNillableBusinessModule(r.businessModuleConverter.ToEntity(api.BusinessModule))
	}

	if api.Id != nil {
		builder.SetID(api.GetId())
	}

	return builder
}

func (r *ApiRepo) BatchCreate(ctx context.Context, apis []*operatorV1.Api) error {
	if len(apis) == 0 {
		return nil
	}

	bulk := make([]*store.ApiCreate, 0, len(apis))
	for _, dto := range apis {
		builder := r.newApiCreate(dto)
		bulk = append(bulk, builder)
	}

	bulkBuilder := r.dbClient.Client().Api.CreateBulk(bulk...)

	if err := bulkBuilder.Exec(ctx); err != nil {
		r.log.Errorf(ctx, "batch insert apis failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("batch insert apis failed")
	}

	return nil
}

func (r *ApiRepo) Update(ctx context.Context, req *operatorV1.UpdateApiRequest) error {
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
			createReq := &operatorV1.CreateApiRequest{Data: req.Data}
			createReq.Data.CreatedBy = createReq.Data.UpdatedBy
			createReq.Data.UpdatedBy = nil
			return r.Create(ctx, createReq)
		}
	}

	builder := r.dbClient.Client().Api.Update()
	err := r.repository.UpdateX(ctx, builder, req.Data, req.GetUpdateMask(),
		func(dto *operatorV1.Api) {
			builder.
				SetNillableDescription(req.Data.Description).
				SetNillableModule(req.Data.Module).
				SetNillableModuleDescription(req.Data.ModuleDescription).
				SetNillableOperation(req.Data.Operation).
				SetNillablePath(req.Data.Path).
				SetNillableMethod(req.Data.Method).
				SetNillableScope(r.scopeConverter.ToEntity(req.Data.Scope)).
				SetNillableUpdatedBy(req.Data.UpdatedBy).
				SetUpdatedAt(time.Now())

			// business_module 为 proto 零值（MODULE_UNSPECIFIED）时跳过：GORM schema 未声明该值，
			// SetNillableBusinessModule 会触发 BusinessModuleValidator 失败。未指定即不更新该字段。
			if req.Data.BusinessModule != nil && *req.Data.BusinessModule != operatorV1.Module_MODULE_UNSPECIFIED {
				builder.SetNillableBusinessModule(r.businessModuleConverter.ToEntity(req.Data.BusinessModule))
			}
		},
		queryscope.Equal(api.FieldID, req.GetId()),
	)

	return err
}

func (r *ApiRepo) Delete(ctx context.Context, req *operatorV1.DeleteApiRequest) error {
	if req == nil {
		return operatorV1.ErrorPermissionBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().Api.Delete()

	_, err := r.repository.Delete(ctx, builder, queryscope.Equal(api.FieldID, req.GetId()))
	if err != nil {
		r.log.Errorf(ctx, "delete api failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("delete api failed")
	}

	return nil
}

// Truncate 清空表数据
func (r *ApiRepo) Truncate(ctx context.Context) error {
	if _, err := r.dbClient.Client().Api.Delete().Exec(ctx); err != nil {
		r.log.Errorf(ctx, "failed to truncate apis table: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("truncate failed")
	}
	return nil
}
