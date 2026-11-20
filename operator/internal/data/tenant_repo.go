package data

import (
	"context"
	"time"

	"github.com/hmlax/muskdeer/operator/internal/data/store/queryscope"

	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	"github.com/tx7do/go-utils/fieldmaskutil"

	"github.com/tx7do/go-utils/copierutil"
	"github.com/tx7do/go-utils/mapper"
	"github.com/tx7do/go-utils/timeutil"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/predicate"
	"github.com/hmlax/muskdeer/operator/internal/data/store/tenant"
)

type TenantRepo struct {
	dbClient *store.DBClient
	log      *bLogger.Helper

	mapper               *mapper.CopierMapper[operatorV1.Tenant, store.Tenant]
	statusConverter      *mapper.EnumTypeConverter[operatorV1.Tenant_Status, tenant.Status]
	typeConverter        *mapper.EnumTypeConverter[operatorV1.Tenant_Type, tenant.Type]
	auditStatusConverter *mapper.EnumTypeConverter[operatorV1.Tenant_AuditStatus, tenant.AuditStatus]

	repository *store.Repository[operatorV1.Tenant, store.Tenant]
}

func NewTenantRepo(ctx *bootstrap.Context, dbClient *store.DBClient) *TenantRepo {
	repo := &TenantRepo{
		log:                  ctx.NewLoggerHelper("tenant/repo/admin-service"),
		dbClient:             dbClient,
		mapper:               mapper.NewCopierMapper[operatorV1.Tenant, store.Tenant](),
		statusConverter:      mapper.NewEnumTypeConverter[operatorV1.Tenant_Status, tenant.Status](operatorV1.Tenant_Status_name, operatorV1.Tenant_Status_value),
		typeConverter:        mapper.NewEnumTypeConverter[operatorV1.Tenant_Type, tenant.Type](operatorV1.Tenant_Type_name, operatorV1.Tenant_Type_value),
		auditStatusConverter: mapper.NewEnumTypeConverter[operatorV1.Tenant_AuditStatus, tenant.AuditStatus](operatorV1.Tenant_AuditStatus_name, operatorV1.Tenant_AuditStatus_value),
	}

	repo.init()

	return repo
}

func (r *TenantRepo) init() {
	r.repository = store.NewRepository[operatorV1.Tenant, store.Tenant](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())

	r.mapper.AppendConverters(r.statusConverter.NewConverterPair())
	r.mapper.AppendConverters(r.typeConverter.NewConverterPair())
	r.mapper.AppendConverters(r.auditStatusConverter.NewConverterPair())
}

func (r *TenantRepo) Count(ctx context.Context, req *paginationV1.PagingRequest) (int, error) {
	builder := r.dbClient.Client().Tenant.Query()

	whereSelectors, _, _ := r.repository.BuildListSelectorWithPaging(builder, req)
	if len(whereSelectors) != 0 {
		builder.Modify(whereSelectors...)
	}

	count, err := builder.Count(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query tenant count failed: %s", err.Error())
		return 0, operatorV1.ErrorIdentityInternalServerError("query count failed")
	}

	return count, nil
}

func (r *TenantRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListTenantResponse, error) {
	if req == nil {
		return nil, operatorV1.ErrorIdentityBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().Tenant.Query()
	countBuilder := r.dbClient.Client().Tenant.Query()

	whereSelectors, _, err := r.repository.BuildListSelectorWithPaging(builder, req)
	if err != nil {
		r.log.Errorf(ctx, "parse list param error [%s]", err.Error())
		return nil, operatorV1.ErrorIdentityBadRequest("invalid query parameter")
	}

	entities, err := builder.All(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query tenant list failed: %s", err.Error())
		return nil, operatorV1.ErrorIdentityInternalServerError("query tenant list failed")
	}

	// plan_id 是边外键（实体上是非导出字段），CopierMapper 拷不进 DTO，
	// 须 WithPlan 预载后从 Edges 回填，否则消费方拿到的 PlanId 恒为 nil。
	dtos := make([]*operatorV1.Tenant, 0, len(entities))
	for _, entity := range entities {
		dto := r.mapper.ToDTO(entity)

		dtos = append(dtos, dto)
	}

	if len(whereSelectors) != 0 {
		countBuilder.Modify(whereSelectors...)
	}
	count, err := countBuilder.Count(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query tenant count failed: %s", err.Error())
		return nil, operatorV1.ErrorIdentityInternalServerError("query count failed")
	}

	return &operatorV1.ListTenantResponse{
		Total: uint64(count),
		Items: dtos,
	}, nil
}

func (r *TenantRepo) IsExist(ctx context.Context, id uint32) (bool, error) {
	exist, err := r.dbClient.Client().Tenant.Query().
		Where(tenant.IDEQ(id)).
		Exist(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query exist failed: %s", err.Error())
		return false, operatorV1.ErrorIdentityInternalServerError("query exist failed")
	}
	return exist, nil
}

func (r *TenantRepo) Get(ctx context.Context, req *operatorV1.GetTenantRequest) (*operatorV1.Tenant, error) {
	if req == nil {
		return nil, operatorV1.ErrorIdentityBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().Tenant.Query()

	var whereCond []queryscope.Scope
	switch req.QueryBy.(type) {
	default:
	case *operatorV1.GetTenantRequest_Id:
		whereCond = append(whereCond, tenant.IDEQ(req.GetId()))

	case *operatorV1.GetTenantRequest_Code:
		whereCond = append(whereCond, tenant.CodeEQ(req.GetCode()))

	case *operatorV1.GetTenantRequest_Name:
		whereCond = append(whereCond, tenant.NameEQ(req.GetName()))
	}

	if len(whereCond) > 0 {
		builder.Modify(whereCond...)
	}

	// repository.Get 泛型实现拿不到 entity 无法回填边，这里按其语义展开
	// （mask 归一化 + 列裁剪 + Only），并保留 entity 供 Edges 回填。
	viewMask := req.GetViewMask()
	fieldmaskutil.NormalizeFieldMaskPaths(viewMask)
	if viewMask != nil && len(viewMask.GetPaths()) > 0 {
		builder.Select(viewMask.GetPaths()...)
	}

	entity, err := builder.Only(ctx)
	if err != nil {
		return nil, err
	}

	dto := r.mapper.ToDTO(entity)

	return dto, nil
}

// BeginTx transfers transaction completion to the calling service. A cleanup
// closure must not capture this method's error instead of the caller's error.
func (r *TenantRepo) BeginTx(ctx context.Context) (*store.Tx, error) {
	tx, err := r.dbClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return nil, operatorV1.ErrorIdentityInternalServerError("start transaction failed")
	}
	return tx, nil
}

func (r *TenantRepo) Create(ctx context.Context, data *operatorV1.Tenant) (tenant *operatorV1.Tenant, err error) {
	if data == nil {
		return nil, operatorV1.ErrorIdentityBadRequest("invalid parameter")
	}

	var tx *store.Tx
	tx, err = r.dbClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return nil, operatorV1.ErrorIdentityInternalServerError("start transaction failed")
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

	return r.CreateWithTx(ctx, tx, data)
}

func (r *TenantRepo) CreateWithTx(ctx context.Context, tx *store.Tx, data *operatorV1.Tenant) (*operatorV1.Tenant, error) {
	if data == nil {
		return nil, operatorV1.ErrorIdentityBadRequest("invalid parameter")
	}

	builder := tx.Tenant.Create().
		SetNillableName(data.Name).
		SetNillableCode(data.Code).
		SetNillableDomain(data.Domain).
		SetNillableLogoURL(data.LogoUrl).
		SetNillableRemark(data.Remark).
		SetNillableIndustry(data.Industry).
		SetNillableAdminUserID(data.AdminUserId).
		SetNillableStatus(r.statusConverter.ToEntity(data.Status)).
		SetNillableType(r.typeConverter.ToEntity(data.Type)).
		SetNillableAuditStatus(r.auditStatusConverter.ToEntity(data.AuditStatus)).
		SetNillableExpiredAt(timeutil.TimestamppbToTime(data.ExpiredAt)).
		SetNillableCreatedBy(data.CreatedBy).
		SetCreatedAt(time.Now())

	if data.Id != nil {
		builder.SetID(data.GetId())
	}

	if ret, err := builder.Save(ctx); err != nil {
		r.log.Errorf(ctx, "insert tenant failed: %s", err.Error())
		return nil, operatorV1.ErrorIdentityInternalServerError("insert tenant failed")
	} else {
		return r.mapper.ToDTO(ret), nil
	}
}

func (r *TenantRepo) Update(ctx context.Context, req *operatorV1.UpdateTenantRequest) error {
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
			createReq := &operatorV1.CreateTenantRequest{Data: req.Data}
			createReq.Data.CreatedBy = createReq.Data.UpdatedBy
			createReq.Data.UpdatedBy = nil
			_, err = r.Create(ctx, createReq.Data)
			return err
		}
	}

	builder := r.dbClient.Client().Tenant.Update()
	err := r.repository.UpdateX(ctx, builder, req.Data, req.GetUpdateMask(),
		func(dto *operatorV1.Tenant) {
			builder.
				SetNillableName(req.Data.Name).
				SetNillableCode(req.Data.Code).
				SetNillableDomain(req.Data.Domain).
				SetNillableLogoURL(req.Data.LogoUrl).
				SetNillableRemark(req.Data.Remark).
				SetNillableIndustry(req.Data.Industry).
				SetNillableAdminUserID(req.Data.AdminUserId).
				SetNillableStatus(r.statusConverter.ToEntity(req.Data.Status)).
				SetNillableType(r.typeConverter.ToEntity(req.Data.Type)).
				SetNillableAuditStatus(r.auditStatusConverter.ToEntity(req.Data.AuditStatus)).
				SetNillableExpiredAt(timeutil.TimestamppbToTime(req.Data.ExpiredAt)).
				SetNillableUpdatedBy(req.Data.UpdatedBy).
				SetUpdatedAt(time.Now())
		},
		queryscope.Equal(tenant.FieldID, req.GetId()),
	)

	return err
}

// AssignTenantAdmin assigns an admin user to a tenant within a transaction.
func (r *TenantRepo) AssignTenantAdmin(ctx context.Context, tx *store.Tx, tenantId uint32, userId uint32) error {
	_, err := tx.Tenant.Update().
		Where(tenant.IDEQ(tenantId)).
		SetAdminUserID(userId).
		Save(ctx)
	if err != nil {
		r.log.Errorf(ctx, "assign tenant admin failed: %s", err.Error())
		return operatorV1.ErrorIdentityInternalServerError("assign tenant admin failed")
	}

	return nil
}

func (r *TenantRepo) Delete(ctx context.Context, req *operatorV1.DeleteTenantRequest) error {
	if req == nil {
		return operatorV1.ErrorIdentityBadRequest("invalid parameter")
	}

	if err := r.dbClient.Client().Tenant.DeleteOneID(req.GetId()).Exec(ctx); err != nil {
		if store.IsNotFound(err) {
			return operatorV1.ErrorIdentityNotFound("tenant not found")
		}

		r.log.Errorf(ctx, "delete one data failed: %s", err.Error())

		return operatorV1.ErrorIdentityInternalServerError("delete failed")
	}

	return nil
}

// TenantExists checks if a tenant with the given code or name exists.
func (r *TenantRepo) TenantExists(ctx context.Context, req *operatorV1.TenantExistsRequest) (*operatorV1.TenantExistsResponse, error) {
	// code 和 name 各自唯一（见 schema 索引），冲突检测应取 OR：
	// 此前用 AND，仅当 code 和 name 同时命中同一行才返回存在，单字段冲突被漏判。
	query := r.dbClient.Client().Tenant.Query()
	predicates := []predicate.Tenant{}
	if code := req.GetCode(); code != "" {
		predicates = append(predicates, tenant.CodeEQ(code))
	}
	if name := req.GetName(); name != "" {
		predicates = append(predicates, tenant.NameEQ(name))
	}
	if len(predicates) > 0 {
		query = query.Where(tenant.Or(predicates...))
	}

	exist, err := query.Exist(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query exist failed: %s", err.Error())
		return nil, operatorV1.ErrorIdentityInternalServerError("query exist failed")
	}

	return &operatorV1.TenantExistsResponse{
		Exist: exist,
	}, nil
}

// ListTenantsByIds gets tenants by a list of IDs.
func (r *TenantRepo) ListTenantsByIds(ctx context.Context, ids []uint32) ([]*operatorV1.Tenant, error) {
	if len(ids) == 0 {
		return []*operatorV1.Tenant{}, nil
	}

	entities, err := r.dbClient.Client().Tenant.Query().
		Where(tenant.IDIn(ids...)).
		All(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query tenant by ids failed: %s", err.Error())
		return nil, operatorV1.ErrorIdentityInternalServerError("query tenant by ids failed")
	}

	dtos := make([]*operatorV1.Tenant, 0, len(entities))
	for _, entity := range entities {
		dto := r.mapper.ToDTO(entity)
		dtos = append(dtos, dto)
	}

	return dtos, nil
}
