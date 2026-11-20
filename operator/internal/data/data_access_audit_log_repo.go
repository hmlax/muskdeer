package data

import (
	"context"
	"time"

	"github.com/hmlax/muskdeer/operator/internal/data/store/queryscope"

	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"

	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/dataaccessauditlog"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"

	"github.com/tx7do/go-utils/copierutil"
	"github.com/tx7do/go-utils/mapper"
)

type DataAccessAuditLogRepo struct {
	dbClient *store.DBClient
	log      *bLogger.Helper

	mapper *mapper.CopierMapper[operatorV1.DataAccessAuditLog, store.DataAccessAuditLog]

	accessTypeConverter     *mapper.EnumTypeConverter[operatorV1.DataAccessAuditLog_AccessType, dataaccessauditlog.AccessType]
	sensitiveLevelConverter *mapper.EnumTypeConverter[operatorV1.SensitiveLevel_SensitiveLevel, dataaccessauditlog.SensitiveLevel]

	repository *store.Repository[operatorV1.DataAccessAuditLog, store.DataAccessAuditLog]
}

func NewDataAccessAuditLogRepo(ctx *bootstrap.Context, dbClient *store.DBClient) *DataAccessAuditLogRepo {
	repo := &DataAccessAuditLogRepo{
		log:      ctx.NewLoggerHelper("data-access-audit-log/repo/admin-service"),
		dbClient: dbClient,
		mapper:   mapper.NewCopierMapper[operatorV1.DataAccessAuditLog, store.DataAccessAuditLog](),
		accessTypeConverter: mapper.NewEnumTypeConverter[operatorV1.DataAccessAuditLog_AccessType, dataaccessauditlog.AccessType](
			operatorV1.DataAccessAuditLog_AccessType_name, operatorV1.DataAccessAuditLog_AccessType_value,
		),
		sensitiveLevelConverter: mapper.NewEnumTypeConverter[operatorV1.SensitiveLevel_SensitiveLevel, dataaccessauditlog.SensitiveLevel](
			operatorV1.SensitiveLevel_SensitiveLevel_name, operatorV1.SensitiveLevel_SensitiveLevel_value,
		),
	}

	repo.init()

	return repo
}

func (r *DataAccessAuditLogRepo) init() {
	r.repository = store.NewRepository[operatorV1.DataAccessAuditLog, store.DataAccessAuditLog](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())

	r.mapper.AppendConverters(r.accessTypeConverter.NewConverterPair())
	r.mapper.AppendConverters(r.sensitiveLevelConverter.NewConverterPair())
}

func (r *DataAccessAuditLogRepo) Count(ctx context.Context, whereCond []queryscope.Scope) (int, error) {
	builder := r.dbClient.Client().DataAccessAuditLog.Query()
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

func (r *DataAccessAuditLogRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListDataAccessAuditLogResponse, error) {
	if req == nil {
		return nil, operatorV1.ErrorAdminBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().DataAccessAuditLog.Query()

	ret, err := r.repository.ListWithPaging(ctx, builder, builder.Clone(), req)
	if err != nil {
		return nil, err
	}
	if ret == nil {
		return &operatorV1.ListDataAccessAuditLogResponse{
			Total: 0,
			Items: nil,
		}, nil
	}

	return &operatorV1.ListDataAccessAuditLogResponse{
		Total: ret.Total,
		Items: ret.Items,
	}, nil
}

func (r *DataAccessAuditLogRepo) IsExist(ctx context.Context, id uint32) (bool, error) {
	exist, err := r.dbClient.Client().DataAccessAuditLog.Query().
		Where(dataaccessauditlog.IDEQ(id)).
		Exist(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query exist failed: %s", err.Error())
		return false, operatorV1.ErrorAdminInternalServerError("query exist failed")
	}
	return exist, nil
}

func (r *DataAccessAuditLogRepo) Get(ctx context.Context, req *operatorV1.GetDataAccessAuditLogRequest) (*operatorV1.DataAccessAuditLog, error) {
	if req == nil {
		return nil, operatorV1.ErrorAdminBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().DataAccessAuditLog.Query()

	var whereCond []queryscope.Scope
	switch req.QueryBy.(type) {
	default:
	case *operatorV1.GetDataAccessAuditLogRequest_Id:
		whereCond = append(whereCond, dataaccessauditlog.IDEQ(req.GetId()))
	}

	dto, err := r.repository.Get(ctx, builder, req.GetViewMask(), whereCond...)
	if err != nil {
		return nil, err
	}

	return dto, err
}

func (r *DataAccessAuditLogRepo) Create(ctx context.Context, req *operatorV1.CreateDataAccessAuditLogRequest) error {
	if req == nil || req.Data == nil {
		return operatorV1.ErrorAdminBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().DataAccessAuditLog.Create().
		SetNillableTenantID(req.Data.TenantId).
		SetNillableUserID(req.Data.UserId).
		SetNillableUsername(req.Data.Username).
		SetNillableIPAddress(req.Data.IpAddress).
		SetNillableRequestID(req.Data.RequestId).
		SetNillableDataSource(req.Data.DataSource).
		SetNillableTableName(req.Data.TableName).
		SetNillableDataID(req.Data.DataId).
		SetNillableAccessType(r.accessTypeConverter.ToEntity(req.Data.AccessType)).
		SetNillableSQLDigest(req.Data.SqlDigest).
		SetNillableSQLText(req.Data.SqlText).
		SetNillableAffectedRows(req.Data.AffectedRows).
		SetNillableLatencyMs(req.Data.LatencyMs).
		SetNillableSuccess(req.Data.Success).
		SetNillableSensitiveLevel(r.sensitiveLevelConverter.ToEntity(req.Data.SensitiveLevel)).
		SetNillableDataMasked(req.Data.DataMasked).
		SetNillableMaskingRules(req.Data.MaskingRules).
		SetNillableBusinessPurpose(req.Data.BusinessPurpose).
		SetNillableDataCategory(req.Data.DataCategory).
		SetNillableDbUser(req.Data.DbUser).
		SetNillableLogHash(req.Data.LogHash).
		SetSignature(req.Data.Signature).
		SetCreatedAt(time.Now())

	if err := builder.Exec(ctx); err != nil {
		r.log.Errorf(ctx, "insert data access audit log failed: %s", err.Error())
		return operatorV1.ErrorAdminInternalServerError("insert data access audit log failed")
	}

	return nil
}
