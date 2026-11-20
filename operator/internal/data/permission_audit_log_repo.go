package data

import (
	"context"
	"time"

	"github.com/hmlax/muskdeer/operator/internal/data/store/queryscope"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	"github.com/tx7do/go-utils/copierutil"
	"github.com/tx7do/go-utils/mapper"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/permissionauditlog"
)

type PermissionAuditLogRepo struct {
	dbClient *store.DBClient
	log      *bLogger.Helper

	mapper              *mapper.CopierMapper[operatorV1.PermissionAuditLog, store.PermissionAuditLog]
	actionTypeConverter *mapper.EnumTypeConverter[operatorV1.PermissionAuditLog_ActionType, permissionauditlog.Action]

	repository *store.Repository[operatorV1.PermissionAuditLog, store.PermissionAuditLog]
}

func NewPermissionAuditLogRepo(ctx *bootstrap.Context, dbClient *store.DBClient) *PermissionAuditLogRepo {
	repo := &PermissionAuditLogRepo{
		log:      ctx.NewLoggerHelper("permission-audit-log/repo/admin-service"),
		dbClient: dbClient,
		mapper:   mapper.NewCopierMapper[operatorV1.PermissionAuditLog, store.PermissionAuditLog](),
		actionTypeConverter: mapper.NewEnumTypeConverter[operatorV1.PermissionAuditLog_ActionType, permissionauditlog.Action](
			operatorV1.PermissionAuditLog_ActionType_name, operatorV1.PermissionAuditLog_ActionType_value,
		),
	}

	repo.init()

	return repo
}

func (r *PermissionAuditLogRepo) init() {
	r.repository = store.NewRepository[operatorV1.PermissionAuditLog, store.PermissionAuditLog](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())

	r.mapper.AppendConverters(r.actionTypeConverter.NewConverterPair())
}

func (r *PermissionAuditLogRepo) Count(ctx context.Context, whereCond []queryscope.Scope) (int, error) {
	builder := r.dbClient.Client().PermissionAuditLog.Query()
	if len(whereCond) != 0 {
		builder.Modify(whereCond...)
	}

	count, err := builder.Count(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query count failed: %s", err.Error())
		return 0, operatorV1.ErrorAuditInternalServerError("query count failed")
	}

	return count, nil
}

func (r *PermissionAuditLogRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListPermissionAuditLogResponse, error) {
	if req == nil {
		return nil, operatorV1.ErrorAuditBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().PermissionAuditLog.Query()

	ret, err := r.repository.ListWithPaging(ctx, builder, builder.Clone(), req)
	if err != nil {
		return nil, err
	}
	if ret == nil {
		return &operatorV1.ListPermissionAuditLogResponse{
			Total: 0,
			Items: nil,
		}, nil
	}

	return &operatorV1.ListPermissionAuditLogResponse{
		Total: ret.Total,
		Items: ret.Items,
	}, nil
}

func (r *PermissionAuditLogRepo) IsExist(ctx context.Context, id uint32) (bool, error) {
	exist, err := r.dbClient.Client().PermissionAuditLog.Query().
		Where(permissionauditlog.IDEQ(id)).
		Exist(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query exist failed: %s", err.Error())
		return false, operatorV1.ErrorAuditInternalServerError("query exist failed")
	}
	return exist, nil
}

func (r *PermissionAuditLogRepo) Get(ctx context.Context, req *operatorV1.GetPermissionAuditLogRequest) (*operatorV1.PermissionAuditLog, error) {
	if req == nil {
		return nil, operatorV1.ErrorAuditBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().PermissionAuditLog.Query()

	var whereCond []queryscope.Scope
	switch req.QueryBy.(type) {
	default:
	case *operatorV1.GetPermissionAuditLogRequest_Id:
		whereCond = append(whereCond, permissionauditlog.IDEQ(req.GetId()))
	}

	dto, err := r.repository.Get(ctx, builder, req.GetViewMask(), whereCond...)
	if err != nil {
		return nil, err
	}

	return dto, err
}

func (r *PermissionAuditLogRepo) Create(ctx context.Context, req *operatorV1.CreatePermissionAuditLogRequest) error {
	if req == nil || req.Data == nil {
		return operatorV1.ErrorAuditBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().PermissionAuditLog.Create().
		SetNillableTenantID(req.Data.TenantId).
		SetNillableOperatorID(req.Data.OperatorId).
		SetNillableOperatorName(req.Data.OperatorName).
		SetNillableTargetID(req.Data.TargetId).
		SetNillableTargetName(req.Data.TargetName).
		SetNillableTargetType(req.Data.TargetType).
		SetNillableAction(r.actionTypeConverter.ToEntity(req.Data.Action)).
		SetNillableOldValue(req.Data.OldValue).
		SetNillableNewValue(req.Data.NewValue).
		SetIPAddress(req.Data.GetIpAddress()).
		SetRequestID(req.Data.GetRequestId()).
		SetReason(req.Data.GetReason()).
		SetNillableLogHash(req.Data.LogHash).
		SetSignature(req.Data.Signature).
		SetCreatedAt(time.Now())

	err := builder.Exec(ctx)
	if err != nil {
		r.log.Errorf(ctx, "insert permission audit log failed: %s", err.Error())
		return operatorV1.ErrorAuditInternalServerError("insert permission audit log failed")
	}

	return err
}
