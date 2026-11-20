package data

import (
	"context"
	"strings"
	"time"

	"github.com/hmlax/muskdeer/operator/internal/data/store/queryscope"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/api"
	"github.com/hmlax/muskdeer/operator/internal/data/store/permissionapi"
	"github.com/hmlax/muskdeer/operator/internal/data/store/permissionpolicy"
	"github.com/hmlax/muskdeer/operator/internal/data/store/policyevaluationlog"
	"github.com/hmlax/muskdeer/operator/internal/data/store/role"
	"github.com/hmlax/muskdeer/operator/internal/data/store/rolepermission"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"

	"github.com/tx7do/go-utils/copierutil"
	"github.com/tx7do/go-utils/mapper"
)

type PolicyEvaluationLogRepo struct {
	dbClient *store.DBClient
	log      *bLogger.Helper

	mapper *mapper.CopierMapper[operatorV1.PolicyEvaluationLog, store.PolicyEvaluationLog]

	repository *store.Repository[operatorV1.PolicyEvaluationLog, store.PolicyEvaluationLog]
}

func NewPolicyEvaluationLogRepo(ctx *bootstrap.Context, dbClient *store.DBClient) *PolicyEvaluationLogRepo {
	repo := &PolicyEvaluationLogRepo{
		log:      ctx.NewLoggerHelper("policy-evaluation-log/repo/admin-service"),
		dbClient: dbClient,
		mapper:   mapper.NewCopierMapper[operatorV1.PolicyEvaluationLog, store.PolicyEvaluationLog](),
	}

	repo.init()

	return repo
}

func (r *PolicyEvaluationLogRepo) init() {
	r.repository = store.NewRepository[operatorV1.PolicyEvaluationLog, store.PolicyEvaluationLog](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())
}

func (r *PolicyEvaluationLogRepo) Count(ctx context.Context, whereCond []queryscope.Scope) (int, error) {
	builder := r.dbClient.Client().PolicyEvaluationLog.Query()
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

func (r *PolicyEvaluationLogRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListPolicyEvaluationLogResponse, error) {
	if req == nil {
		return nil, operatorV1.ErrorPermissionBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().PolicyEvaluationLog.Query()

	ret, err := r.repository.ListWithPaging(ctx, builder, builder.Clone(), req)
	if err != nil {
		return nil, err
	}
	if ret == nil {
		return &operatorV1.ListPolicyEvaluationLogResponse{
			Total: 0,
			Items: nil,
		}, nil
	}

	return &operatorV1.ListPolicyEvaluationLogResponse{
		Total: ret.Total,
		Items: ret.Items,
	}, nil
}

func (r *PolicyEvaluationLogRepo) IsExist(ctx context.Context, id uint32) (bool, error) {
	exist, err := r.dbClient.Client().PolicyEvaluationLog.Query().
		Where(policyevaluationlog.IDEQ(id)).
		Exist(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query exist failed: %s", err.Error())
		return false, operatorV1.ErrorPermissionInternalServerError("query exist failed")
	}
	return exist, nil
}

func (r *PolicyEvaluationLogRepo) Get(ctx context.Context, req *operatorV1.GetPolicyEvaluationLogRequest) (*operatorV1.PolicyEvaluationLog, error) {
	if req == nil {
		return nil, operatorV1.ErrorPermissionBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().PolicyEvaluationLog.Query()

	var whereCond []queryscope.Scope
	switch req.QueryBy.(type) {
	default:
	case *operatorV1.GetPolicyEvaluationLogRequest_Id:
		whereCond = append(whereCond, policyevaluationlog.IDEQ(req.GetId()))
	}

	dto, err := r.repository.Get(ctx, builder, req.GetViewMask(), whereCond...)
	if err != nil {
		return nil, err
	}

	return dto, err
}

func (r *PolicyEvaluationLogRepo) Create(ctx context.Context, req *operatorV1.CreatePolicyEvaluationLogRequest) error {
	if req == nil || req.Data == nil {
		return operatorV1.ErrorPermissionBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().PolicyEvaluationLog.
		Create().
		SetNillableTenantID(req.Data.TenantId).
		SetUserID(req.Data.GetUserId()).
		SetMembershipID(req.Data.GetMembershipId()).
		SetPermissionID(req.Data.GetPermissionId()).
		SetNillablePolicyID(req.Data.PolicyId).
		SetNillableRequestPath(req.Data.RequestPath).
		SetNillableRequestMethod(req.Data.RequestMethod).
		SetNillableResult(req.Data.Result).
		SetNillableEffectDetails(req.Data.EffectDetails).
		SetNillableScopeSQL(req.Data.ScopeSql).
		SetIPAddress(req.Data.GetIpAddress()).
		SetNillableTraceID(req.Data.TraceId).
		SetNillableEvaluationContext(req.Data.EvaluationContext).
		SetNillableLogHash(req.Data.LogHash).
		SetSignature(req.Data.Signature).
		SetCreatedAt(time.Now())

	err := builder.Exec(ctx)
	if err != nil {
		r.log.Errorf(ctx, "insert policy evaluation log failed: %s", err.Error())
		return operatorV1.ErrorPermissionInternalServerError("insert policy evaluation log failed")
	}

	return err
}

// ResolvePermissionPolicyByRoute 反查评估命中的权限点与挂接策略：
// (角色码, 路径模板, HTTP方法) → sys_apis → sys_permission_apis → sys_permissions
// （角色经 sys_role_permissions 持有该权限）→ sys_permission_policies 按 eval_order 首条。
// 任一环缺数据（API 未登记 / 角色未授权 / 无策略行）对应 ID 返回 0，不报错——
// 评估埋点尽力而为，绝不影响鉴权主流程。调用方需自行做 TTL 缓存。
// 四步各走索引的单表查询（曾试过一条 Modify 联表，pgx 参数类型推断 42P18）。
func (r *PolicyEvaluationLogRepo) ResolvePermissionPolicyByRoute(ctx context.Context, roleCode, path, method string) (permissionID uint32, policyID uint32) {
	if roleCode == "" || path == "" || method == "" {
		return 0, 0
	}
	client := r.dbClient.Client()

	apiID, err := client.Api.Query().
		Where(api.PathEQ(path), api.MethodEQ(strings.ToUpper(method))).
		FirstID(ctx)
	if err != nil {
		return 0, 0
	}

	pa, err := client.PermissionApi.Query().
		Where(permissionapi.APIIDEQ(apiID)).
		First(ctx)
	if err != nil {
		return 0, 0
	}
	if pa.PermissionID != nil {
		permissionID = *pa.PermissionID
	}

	// 校验该角色确实持有此权限（经 sys_role_permissions）
	roleID, err := client.Role.Query().
		Where(role.CodeEQ(roleCode)).
		FirstID(ctx)
	if err != nil {
		return 0, 0
	}
	held, err := client.RolePermission.Query().
		Where(rolepermission.RoleIDEQ(roleID), rolepermission.PermissionIDEQ(permissionID)).
		Exist(ctx)
	if err != nil || !held {
		return 0, 0
	}

	policyID, err = client.PermissionPolicy.Query().
		Where(permissionpolicy.PermissionIDEQ(permissionID)).
		Order(store.Asc(permissionpolicy.FieldEvalOrder)).
		FirstID(ctx)
	if err != nil {
		return 0, permissionID
	}
	return permissionID, policyID
}
