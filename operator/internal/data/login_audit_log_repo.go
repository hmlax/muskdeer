package data

import (
	"context"
	"time"

	"github.com/hmlax/muskdeer/operator/internal/data/store/queryscope"

	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"

	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/loginauditlog"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"

	"github.com/tx7do/go-utils/copierutil"
	"github.com/tx7do/go-utils/mapper"
)

type LoginAuditLogRepo struct {
	dbClient *store.DBClient
	log      *bLogger.Helper

	mapper *mapper.CopierMapper[operatorV1.LoginAuditLog, store.LoginAuditLog]

	statusConverter      *mapper.EnumTypeConverter[operatorV1.LoginAuditLog_Status, loginauditlog.Status]
	actionTypeConverter  *mapper.EnumTypeConverter[operatorV1.LoginAuditLog_ActionType, loginauditlog.ActionType]
	riskLevelConverter   *mapper.EnumTypeConverter[operatorV1.LoginAuditLog_RiskLevel, loginauditlog.RiskLevel]
	loginMethodConverter *mapper.EnumTypeConverter[operatorV1.LoginAuditLog_LoginMethod, loginauditlog.LoginMethod]

	repository *store.Repository[operatorV1.LoginAuditLog, store.LoginAuditLog]
}

func NewLoginAuditLogRepo(ctx *bootstrap.Context, dbClient *store.DBClient) *LoginAuditLogRepo {
	repo := &LoginAuditLogRepo{
		log:      ctx.NewLoggerHelper("login-audit-log/repo/admin-service"),
		dbClient: dbClient,
		mapper:   mapper.NewCopierMapper[operatorV1.LoginAuditLog, store.LoginAuditLog](),
		statusConverter: mapper.NewEnumTypeConverter[operatorV1.LoginAuditLog_Status, loginauditlog.Status](
			operatorV1.LoginAuditLog_Status_name, operatorV1.LoginAuditLog_Status_value,
		),
		actionTypeConverter: mapper.NewEnumTypeConverter[operatorV1.LoginAuditLog_ActionType, loginauditlog.ActionType](
			operatorV1.LoginAuditLog_ActionType_name, operatorV1.LoginAuditLog_ActionType_value,
		),
		riskLevelConverter: mapper.NewEnumTypeConverter[operatorV1.LoginAuditLog_RiskLevel, loginauditlog.RiskLevel](
			operatorV1.LoginAuditLog_RiskLevel_name, operatorV1.LoginAuditLog_RiskLevel_value,
		),
		loginMethodConverter: mapper.NewEnumTypeConverter[operatorV1.LoginAuditLog_LoginMethod, loginauditlog.LoginMethod](
			operatorV1.LoginAuditLog_LoginMethod_name, operatorV1.LoginAuditLog_LoginMethod_value,
		),
	}

	repo.init()

	return repo
}

func (r *LoginAuditLogRepo) init() {
	r.repository = store.NewRepository[operatorV1.LoginAuditLog, store.LoginAuditLog](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())

	r.mapper.AppendConverters(r.statusConverter.NewConverterPair())
	r.mapper.AppendConverters(r.actionTypeConverter.NewConverterPair())
	r.mapper.AppendConverters(r.riskLevelConverter.NewConverterPair())
	// 此前漏注册 loginMethodConverter：写入时直接调 converter 故正常，
	// 但读回（Get/List）走共享 mapper，login_method 字段恒为零值。补注册。
	r.mapper.AppendConverters(r.loginMethodConverter.NewConverterPair())
}

func (r *LoginAuditLogRepo) Count(ctx context.Context, whereCond []queryscope.Scope) (int, error) {
	builder := r.dbClient.Client().LoginAuditLog.Query()
	if len(whereCond) != 0 {
		builder.Modify(whereCond...)
	}

	count, err := builder.Count(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query count failed: %s", err.Error())
		return 0, operatorV1.ErrorAdminInternalServerError("query count failed")
	}

	return count, nil
}

func (r *LoginAuditLogRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListLoginAuditLogResponse, error) {
	if req == nil {
		return nil, operatorV1.ErrorAdminBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().LoginAuditLog.Query()

	ret, err := r.repository.ListWithPaging(ctx, builder, builder.Clone(), req)
	if err != nil {
		return nil, err
	}
	if ret == nil {
		return &operatorV1.ListLoginAuditLogResponse{
			Total: 0,
			Items: nil,
		}, nil
	}

	return &operatorV1.ListLoginAuditLogResponse{
		Total: ret.Total,
		Items: ret.Items,
	}, nil
}

func (r *LoginAuditLogRepo) IsExist(ctx context.Context, id uint32) (bool, error) {
	exist, err := r.dbClient.Client().LoginAuditLog.Query().
		Where(loginauditlog.IDEQ(id)).
		Exist(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query exist failed: %s", err.Error())
		return false, operatorV1.ErrorAdminInternalServerError("query exist failed")
	}
	return exist, nil
}

func (r *LoginAuditLogRepo) Get(ctx context.Context, req *operatorV1.GetLoginAuditLogRequest) (*operatorV1.LoginAuditLog, error) {
	if req == nil {
		return nil, operatorV1.ErrorAdminBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().LoginAuditLog.Query()

	var whereCond []queryscope.Scope
	switch req.QueryBy.(type) {
	default:
	case *operatorV1.GetLoginAuditLogRequest_Id:
		whereCond = append(whereCond, loginauditlog.IDEQ(req.GetId()))
	}

	dto, err := r.repository.Get(ctx, builder, req.GetViewMask(), whereCond...)
	if err != nil {
		return nil, err
	}

	return dto, err
}

func (r *LoginAuditLogRepo) Create(ctx context.Context, req *operatorV1.CreateLoginAuditLogRequest) error {
	if req == nil || req.Data == nil {
		return operatorV1.ErrorAdminBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().LoginAuditLog.Create().
		SetNillableTenantID(req.Data.TenantId).
		SetNillableUserID(req.Data.UserId).
		SetNillableUsername(req.Data.Username).
		SetNillableIPAddress(req.Data.IpAddress).
		SetGeoLocation(req.Data.GeoLocation).
		SetNillableSessionID(req.Data.SessionId).
		SetDeviceInfo(req.Data.DeviceInfo).
		SetNillableRequestID(req.Data.RequestId).
		SetNillableTraceID(req.Data.TraceId).
		SetNillableActionType(r.actionTypeConverter.ToEntity(req.Data.ActionType)).
		SetNillableStatus(r.statusConverter.ToEntity(req.Data.Status)).
		SetNillableLoginMethod(r.loginMethodConverter.ToEntity(req.Data.LoginMethod)).
		SetNillableFailureReason(req.Data.FailureReason).
		SetNillableMfaStatus(req.Data.MfaStatus).
		SetNillableRiskScore(req.Data.RiskScore).
		SetNillableRiskLevel(r.riskLevelConverter.ToEntity(req.Data.RiskLevel)).
		SetRiskFactors(req.Data.RiskFactors).
		SetNillableLogHash(req.Data.LogHash).
		SetSignature(req.Data.Signature).
		SetCreatedAt(time.Now())

	if err := builder.Exec(ctx); err != nil {
		r.log.Errorf(ctx, "insert login audit log failed: %s", err.Error())
		return operatorV1.ErrorAdminInternalServerError("insert login audit log failed")
	}

	return nil
}
