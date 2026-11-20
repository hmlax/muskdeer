package data

import (
	"context"
	"time"

	"github.com/hmlax/muskdeer/operator/internal/data/store/queryscope"

	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"

	"github.com/tx7do/go-utils/copierutil"
	"github.com/tx7do/go-utils/mapper"
	"github.com/tx7do/go-utils/timeutil"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/position"
)

type PositionRepo struct {
	dbClient *store.DBClient
	log      *bLogger.Helper

	mapper          *mapper.CopierMapper[operatorV1.Position, store.Position]
	statusConverter *mapper.EnumTypeConverter[operatorV1.Position_Status, position.Status]
	typeConverter   *mapper.EnumTypeConverter[operatorV1.Position_Type, position.Type]

	repository *store.Repository[operatorV1.Position, store.Position]
}

func NewPositionRepo(ctx *bootstrap.Context, dbClient *store.DBClient) *PositionRepo {
	repo := &PositionRepo{
		log:      ctx.NewLoggerHelper("position/repo/admin-service"),
		dbClient: dbClient,
		mapper:   mapper.NewCopierMapper[operatorV1.Position, store.Position](),
		statusConverter: mapper.NewEnumTypeConverter[operatorV1.Position_Status, position.Status](
			operatorV1.Position_Status_name, operatorV1.Position_Status_value,
		),
		typeConverter: mapper.NewEnumTypeConverter[operatorV1.Position_Type, position.Type](
			operatorV1.Position_Type_name, operatorV1.Position_Type_value,
		),
	}

	repo.init()

	return repo
}

func (r *PositionRepo) init() {
	r.repository = store.NewRepository[operatorV1.Position, store.Position](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())

	r.mapper.AppendConverters(r.statusConverter.NewConverterPair())
	r.mapper.AppendConverters(r.typeConverter.NewConverterPair())
}

func (r *PositionRepo) Count(ctx context.Context, req *paginationV1.PagingRequest) (int, error) {
	builder := r.dbClient.Client().Position.Query()

	whereSelectors, _, _ := r.repository.BuildListSelectorWithPaging(builder, req)
	if len(whereSelectors) != 0 {
		builder.Modify(whereSelectors...)
	}

	count, err := builder.Count(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query position count failed: %s", err.Error())
		return 0, operatorV1.ErrorIdentityInternalServerError("query count failed")
	}

	return count, nil
}

func (r *PositionRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListPositionResponse, error) {
	if req == nil {
		return nil, operatorV1.ErrorIdentityBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().Position.Query()

	ret, err := r.repository.ListWithPaging(ctx, builder, builder.Clone(), req)
	if err != nil {
		return nil, err
	}
	if ret == nil {
		return &operatorV1.ListPositionResponse{
			Total: 0,
			Items: nil,
		}, nil
	}

	r.queryEnumsAndBackfill(ctx, ret.Items)

	return &operatorV1.ListPositionResponse{
		Total: ret.Total,
		Items: ret.Items,
	}, nil
}

func (r *PositionRepo) IsExist(ctx context.Context, id uint32) (bool, error) {
	exist, err := r.dbClient.Client().Position.Query().
		Where(position.IDEQ(id)).
		Exist(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query exist failed: %s", err.Error())
		return false, operatorV1.ErrorIdentityInternalServerError("query exist failed")
	}
	return exist, nil
}

func (r *PositionRepo) Get(ctx context.Context, req *operatorV1.GetPositionRequest) (*operatorV1.Position, error) {
	if req == nil {
		return nil, operatorV1.ErrorIdentityBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().Position.Query()

	var whereCond []queryscope.Scope
	switch req.QueryBy.(type) {
	default:
	case *operatorV1.GetPositionRequest_Id:
		whereCond = append(whereCond, position.IDEQ(req.GetId()))
	case *operatorV1.GetPositionRequest_Name:
		// position.name 连租户内都不唯一，平台上下文(tid=0)下按 name 查询必然跨租户匹配多行，
		// .Only() 会报 not singular。仅允许在具名租户上下文(tid>0)下按 name 查询。
		if _, hasTenant := maybeTenantFromViewer(ctx); !hasTenant {
			return nil, operatorV1.ErrorIdentityBadRequest("tenant scope required to query position by name")
		}
		whereCond = append(whereCond, position.NameEQ(req.GetName()))
	case *operatorV1.GetPositionRequest_Code:
		// position.code 仅在 (tenant_id, code) 维度唯一，平台上下文(tid=0)下按 code 查询
		// 会跨租户匹配多行导致 .Only() 报 not singular。仅允许在具名租户上下文(tid>0)下按 code 查询。
		if _, hasTenant := maybeTenantFromViewer(ctx); !hasTenant {
			return nil, operatorV1.ErrorIdentityBadRequest("tenant scope required to query position by code")
		}
		whereCond = append(whereCond, position.CodeEQ(req.GetCode()))
	}

	dto, err := r.repository.Get(ctx, builder, req.GetViewMask(), whereCond...)
	if err != nil {
		return nil, err
	}

	r.queryEnumsAndBackfill(ctx, []*operatorV1.Position{dto})

	return dto, err
}

// queryEnumsAndBackfill 查询枚举列并回填 DTO 的 type 字段。
//
// 枚举转换机制注记：mapper 经 EnumTypeConverter.NewConverterPair 注册的
// 是**指针↔指针对**（*ent枚举 → *proto枚举）——实体侧可空指针枚举列
// （Optional+Nillable，如本仓 status）在 copier 的转换查表里精确命中、
// 读路径本就如实流通，无需回填。被丢弃的是**混合形态**：实体侧为值型
// 枚举列（带 Default 且无 Nillable，如本仓 type）而 DTO 侧为可选指针
// 字段——值型源与指针对键失配，copier 转而给 DTO 指针分配零值。故此处
// 只对 type 做二次查询回填（对齐 notification_channel 的 Type 同型修复）。
func (r *PositionRepo) queryEnumsAndBackfill(ctx context.Context, items []*operatorV1.Position) {
	if len(items) == 0 {
		return
	}
	entities, err := r.dbClient.Client().Position.Query().
		Select(position.FieldID, position.FieldType).
		All(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query position enum columns failed: %s", err.Error())
		return
	}
	r.backfillEnumsFrom(items, entities)
}

func (r *PositionRepo) backfillEnumsFrom(items []*operatorV1.Position, entities []*store.Position) {
	if len(items) == 0 || len(entities) == 0 {
		return
	}
	types := make(map[uint32]position.Type, len(entities))
	for _, e := range entities {
		if e.Type != "" {
			types[e.ID] = e.Type
		}
	}
	for _, it := range items {
		if t, ok := types[it.GetId()]; ok {
			tv := t
			if p := r.typeConverter.ToDTO(&tv); p != nil {
				it.Type = p
			}
		}
	}
}

// ListPositionByIds 通过多个ID获取职位信息
func (r *PositionRepo) ListPositionByIds(ctx context.Context, ids []uint32) ([]*operatorV1.Position, error) {
	if len(ids) == 0 {
		return []*operatorV1.Position{}, nil
	}

	entities, err := r.dbClient.Client().Position.Query().
		Where(position.IDIn(ids...)).
		All(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query position by ids failed: %s", err.Error())
		return nil, operatorV1.ErrorIdentityInternalServerError("query position by ids failed")
	}

	dtos := make([]*operatorV1.Position, 0, len(entities))
	for _, entity := range entities {
		dto := r.mapper.ToDTO(entity)
		dtos = append(dtos, dto)
	}

	r.backfillEnumsFrom(dtos, entities)

	return dtos, nil
}

func (r *PositionRepo) Create(ctx context.Context, req *operatorV1.CreatePositionRequest) error {
	if req == nil || req.Data == nil {
		return operatorV1.ErrorIdentityBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().Position.Create().
		SetNillableTenantID(req.Data.TenantId).
		SetName(req.Data.GetName()).
		SetCode(req.Data.GetCode()).
		SetOrgUnitID(req.Data.GetOrgUnitId()).
		SetReportsToPositionID(req.Data.GetReportsToPositionId()).
		SetNillableSortOrder(req.Data.SortOrder).
		SetNillableStatus(r.statusConverter.ToEntity(req.Data.Status)).
		SetNillableType(r.typeConverter.ToEntity(req.Data.Type)).
		SetNillableJobFamily(req.Data.JobFamily).
		SetNillableJobGrade(req.Data.JobGrade).
		SetNillableLevel(req.Data.Level).
		SetNillableIsKeyPosition(req.Data.IsKeyPosition).
		SetNillableHeadcount(req.Data.Headcount).
		SetNillableDescription(req.Data.Description).
		SetNillableRemark(req.Data.Remark).
		SetNillableStartAt(timeutil.TimestamppbToTime(req.Data.StartAt)).
		SetNillableEndAt(timeutil.TimestamppbToTime(req.Data.EndAt)).
		SetNillableCreatedBy(req.Data.CreatedBy).
		SetCreatedAt(time.Now())

	if req.Data.Id != nil {
		builder.SetID(req.GetData().GetId())
	}

	if err := builder.Exec(ctx); err != nil {
		r.log.Errorf(ctx, "insert position failed: %s", err.Error())
		return operatorV1.ErrorIdentityInternalServerError("insert data failed")
	}

	return nil
}

func (r *PositionRepo) Update(ctx context.Context, req *operatorV1.UpdatePositionRequest) error {
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
			createReq := &operatorV1.CreatePositionRequest{Data: req.Data}
			createReq.Data.CreatedBy = createReq.Data.UpdatedBy
			createReq.Data.UpdatedBy = nil
			return r.Create(ctx, createReq)
		}
	}

	builder := r.dbClient.Client().Position.Update()
	err := r.repository.UpdateX(ctx, builder, req.Data, req.GetUpdateMask(),
		func(dto *operatorV1.Position) {
			builder.
				SetNillableName(req.Data.Name).
				SetNillableCode(req.Data.Code).
				SetNillableOrgUnitID(req.Data.OrgUnitId).
				SetNillableReportsToPositionID(req.Data.ReportsToPositionId).
				SetNillableSortOrder(req.Data.SortOrder).
				SetNillableStatus(r.statusConverter.ToEntity(req.Data.Status)).
				SetNillableType(r.typeConverter.ToEntity(req.Data.Type)).
				SetNillableJobFamily(req.Data.JobFamily).
				SetNillableJobGrade(req.Data.JobGrade).
				SetNillableLevel(req.Data.Level).
				SetNillableIsKeyPosition(req.Data.IsKeyPosition).
				SetNillableHeadcount(req.Data.Headcount).
				SetNillableDescription(req.Data.Description).
				SetNillableRemark(req.Data.Remark).
				SetNillableStartAt(timeutil.TimestamppbToTime(req.Data.StartAt)).
				SetNillableEndAt(timeutil.TimestamppbToTime(req.Data.EndAt)).
				SetNillableUpdatedBy(req.Data.UpdatedBy).
				SetUpdatedAt(time.Now())
		},
		queryscope.Equal(position.FieldID, req.GetId()),
	)

	return err
}

func (r *PositionRepo) Delete(ctx context.Context, req *operatorV1.DeletePositionRequest) error {
	if req == nil {
		return operatorV1.ErrorIdentityBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().Position.Delete()

	var err error
	_, err = r.repository.Delete(ctx, builder, queryscope.Equal(position.FieldID, req.GetId()))
	if err != nil {
		r.log.Errorf(ctx, "delete position failed: %s", err.Error())
		return operatorV1.ErrorIdentityInternalServerError("delete position failed")
	}

	return nil
}
