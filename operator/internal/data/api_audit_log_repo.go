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

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/apiauditlog"
)

type ApiAuditLogRepo struct {
	dbClient *store.DBClient
	log      *bLogger.Helper

	mapper *mapper.CopierMapper[operatorV1.ApiAuditLog, store.ApiAuditLog]

	repository *store.Repository[operatorV1.ApiAuditLog, store.ApiAuditLog]
}

func NewApiAuditLogRepo(ctx *bootstrap.Context, dbClient *store.DBClient) *ApiAuditLogRepo {
	repo := &ApiAuditLogRepo{
		log:      ctx.NewLoggerHelper("api-audit-log/repo/admin-service"),
		dbClient: dbClient,
		mapper:   mapper.NewCopierMapper[operatorV1.ApiAuditLog, store.ApiAuditLog](),
	}

	repo.init()

	return repo
}

func (r *ApiAuditLogRepo) init() {
	r.repository = store.NewRepository[operatorV1.ApiAuditLog, store.ApiAuditLog](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())

}

func (r *ApiAuditLogRepo) Count(ctx context.Context, whereCond []queryscope.Scope) (int, error) {
	builder := r.dbClient.Client().ApiAuditLog.Query()
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

func (r *ApiAuditLogRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListApiAuditLogResponse, error) {
	if req == nil {
		return nil, operatorV1.ErrorAdminBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().ApiAuditLog.Query()

	ret, err := r.repository.ListWithPaging(ctx, builder, builder.Clone(), req)
	if err != nil {
		return nil, err
	}
	if ret == nil {
		return &operatorV1.ListApiAuditLogResponse{
			Total: 0,
			Items: nil,
		}, nil
	}

	return &operatorV1.ListApiAuditLogResponse{
		Total: ret.Total,
		Items: ret.Items,
	}, nil
}

func (r *ApiAuditLogRepo) IsExist(ctx context.Context, id uint32) (bool, error) {
	exist, err := r.dbClient.Client().ApiAuditLog.Query().
		Where(apiauditlog.IDEQ(id)).
		Exist(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query exist failed: %s", err.Error())
		return false, operatorV1.ErrorAdminInternalServerError("query exist failed")
	}
	return exist, nil
}

func (r *ApiAuditLogRepo) Get(ctx context.Context, req *operatorV1.GetApiAuditLogRequest) (*operatorV1.ApiAuditLog, error) {
	if req == nil {
		return nil, operatorV1.ErrorAdminBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().ApiAuditLog.Query()

	var whereCond []queryscope.Scope
	switch req.QueryBy.(type) {
	default:
	case *operatorV1.GetApiAuditLogRequest_Id:
		whereCond = append(whereCond, apiauditlog.IDEQ(req.GetId()))
	}

	dto, err := r.repository.Get(ctx, builder, req.GetViewMask(), whereCond...)
	if err != nil {
		return nil, err
	}

	return dto, err
}

func (r *ApiAuditLogRepo) Create(ctx context.Context, req *operatorV1.CreateApiAuditLogRequest) error {
	if req == nil || req.Data == nil {
		return operatorV1.ErrorAdminBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().ApiAuditLog.Create().
		SetNillableTenantID(req.Data.TenantId).
		SetNillableUserID(req.Data.UserId).
		SetNillableUsername(req.Data.Username).
		SetNillableIPAddress(req.Data.IpAddress).
		SetGeoLocation(req.Data.GeoLocation).
		SetDeviceInfo(req.Data.DeviceInfo).
		SetNillableReferer(req.Data.Referer).
		SetNillableAppVersion(req.Data.AppVersion).
		SetNillableHTTPMethod(req.Data.HttpMethod).
		SetNillablePath(req.Data.Path).
		SetNillableRequestURI(req.Data.RequestUri).
		SetNillableAPIModule(req.Data.ApiModule).
		SetNillableAPIOperation(req.Data.ApiOperation).
		SetNillableAPIDescription(req.Data.ApiDescription).
		SetNillableRequestID(req.Data.RequestId).
		SetNillableTraceID(req.Data.TraceId).
		SetNillableSpanID(req.Data.SpanId).
		SetNillableLatencyMs(req.Data.LatencyMs).
		SetNillableSuccess(req.Data.Success).
		SetNillableStatusCode(req.Data.StatusCode).
		SetNillableReason(req.Data.Reason).
		SetNillableRequestHeader(req.Data.RequestHeader).
		SetNillableRequestBody(req.Data.RequestBody).
		SetNillableResponse(req.Data.Response).
		SetNillableLogHash(req.Data.LogHash).
		SetSignature(req.Data.Signature).
		SetCreatedAt(time.Now())

	err := builder.Exec(ctx)
	if err != nil {
		r.log.Errorf(ctx, "insert api audit log failed: %s", err.Error())
		return operatorV1.ErrorAdminInternalServerError("insert api audit log failed")
	}

	return err
}
