package data

import (
	"context"
	"fmt"
	"time"

	"github.com/hmlax/muskdeer/operator/internal/data/store/queryscope"

	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"

	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/loginpolicy"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"

	"github.com/tx7do/go-utils/copierutil"
	"github.com/tx7do/go-utils/mapper"
)

type LoginPolicyRepo struct {
	dbClient *store.DBClient
	log      *bLogger.Helper

	mapper          *mapper.CopierMapper[operatorV1.LoginPolicy, store.LoginPolicy]
	typeConverter   *mapper.EnumTypeConverter[operatorV1.LoginPolicy_Type, loginpolicy.Type]
	methodConverter *mapper.EnumTypeConverter[operatorV1.LoginPolicy_Method, loginpolicy.Method]

	repository *store.Repository[operatorV1.LoginPolicy, store.LoginPolicy]
}

func NewLoginPolicyRepo(ctx *bootstrap.Context, dbClient *store.DBClient) *LoginPolicyRepo {
	repo := &LoginPolicyRepo{
		log:      ctx.NewLoggerHelper("login-policy/repo/admin-service"),
		dbClient: dbClient,
		mapper:   mapper.NewCopierMapper[operatorV1.LoginPolicy, store.LoginPolicy](),
		typeConverter: mapper.NewEnumTypeConverter[operatorV1.LoginPolicy_Type, loginpolicy.Type](
			operatorV1.LoginPolicy_Type_name, operatorV1.LoginPolicy_Type_value,
		),
		methodConverter: mapper.NewEnumTypeConverter[operatorV1.LoginPolicy_Method, loginpolicy.Method](
			operatorV1.LoginPolicy_Method_name, operatorV1.LoginPolicy_Method_value,
		),
	}

	repo.init()

	return repo
}

func (r *LoginPolicyRepo) init() {
	r.repository = store.NewRepository[operatorV1.LoginPolicy, store.LoginPolicy](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())

	r.mapper.AppendConverters(r.typeConverter.NewConverterPair())
	r.mapper.AppendConverters(r.methodConverter.NewConverterPair())
}

// EffectivePolicy 登录闸门用的策略条目（GORM 实体的精简视图，避免服务层依赖生成代码）。
type EffectivePolicy struct {
	TargetID uint32 // 0 表示全局策略（不限定用户）
	Value    string
	Type     string // BLACKLIST / WHITELIST
	Method   string // IP / MAC / REGION / TIME / DEVICE
	Reason   string
}

// ListForLogin 拉取租户内全部登录策略，供登录闸门在内存中按
// 全局（target_id 为空）/ 用户定向（target_id = userId）两批匹配。
// 策略量级小（管理配置项），全量拉取 + 内存过滤即可，无需按用户建索引。
func (r *LoginPolicyRepo) ListForLogin(ctx context.Context, tenantID uint32) ([]EffectivePolicy, error) {
	entities, err := r.dbClient.Client().LoginPolicy.Query().
		Where(loginpolicy.TenantIDEQ(tenantID)).
		All(ctx)
	if err != nil {
		r.log.Errorf(ctx, "list login policies for login failed: %s", err.Error())
		return nil, fmt.Errorf("list login policies failed")
	}
	policies := make([]EffectivePolicy, 0, len(entities))
	for _, e := range entities {
		policies = append(policies, EffectivePolicy{
			TargetID: derefUint32(e.TargetID),
			Value:    derefStr(e.Value),
			Type:     derefStrP(e.Type),
			Method:   derefStrP(e.Method),
			Reason:   derefStr(e.Reason),
		})
	}
	return policies, nil
}

func derefUint32(p *uint32) uint32 {
	if p == nil {
		return 0
	}
	return *p
}

func derefStrP[T ~string](p *T) string {
	if p == nil {
		return ""
	}
	return string(*p)
}

func (r *LoginPolicyRepo) Count(ctx context.Context, whereCond []queryscope.Scope) (int, error) {
	builder := r.dbClient.Client().LoginPolicy.Query()
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

func (r *LoginPolicyRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListLoginPolicyResponse, error) {
	if req == nil {
		return nil, operatorV1.ErrorAdminBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().LoginPolicy.Query()

	ret, err := r.repository.ListWithPaging(ctx, builder, builder.Clone(), req)
	if err != nil {
		return nil, err
	}
	if ret == nil {
		return &operatorV1.ListLoginPolicyResponse{
			Total: 0,
			Items: nil,
		}, nil
	}

	return &operatorV1.ListLoginPolicyResponse{
		Total: ret.Total,
		Items: ret.Items,
	}, nil
}

func (r *LoginPolicyRepo) IsExist(ctx context.Context, id uint32) (bool, error) {
	exist, err := r.dbClient.Client().LoginPolicy.Query().
		Where(loginpolicy.IDEQ(id)).
		Exist(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query exist failed: %s", err.Error())
		return false, operatorV1.ErrorAdminInternalServerError("query exist failed")
	}
	return exist, nil
}

func (r *LoginPolicyRepo) Get(ctx context.Context, req *operatorV1.GetLoginPolicyRequest) (*operatorV1.LoginPolicy, error) {
	if req == nil {
		return nil, operatorV1.ErrorAdminBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().LoginPolicy.Query()

	var whereCond []queryscope.Scope
	switch req.QueryBy.(type) {
	default:
	case *operatorV1.GetLoginPolicyRequest_Id:
		whereCond = append(whereCond, loginpolicy.IDEQ(req.GetId()))
	}

	dto, err := r.repository.Get(ctx, builder, req.GetViewMask(), whereCond...)
	if err != nil {
		return nil, err
	}

	return dto, err
}

func (r *LoginPolicyRepo) Create(ctx context.Context, req *operatorV1.CreateLoginPolicyRequest) error {
	if req == nil || req.Data == nil {
		return operatorV1.ErrorAdminBadRequest("invalid request")
	}

	builder := r.dbClient.Client().LoginPolicy.Create().
		SetNillableTenantID(req.Data.TenantId).
		SetNillableTargetID(req.Data.TargetId).
		SetNillableType(r.typeConverter.ToEntity(req.Data.Type)).
		SetNillableMethod(r.methodConverter.ToEntity(req.Data.Method)).
		SetNillableValue(req.Data.Value).
		SetNillableReason(req.Data.Reason).
		SetNillableCreatedBy(req.Data.CreatedBy).
		SetCreatedAt(time.Now())

	if err := builder.Exec(ctx); err != nil {
		r.log.Errorf(ctx, "insert admin login restriction failed: %s", err.Error())
		return operatorV1.ErrorAdminInternalServerError("insert admin login restriction failed")
	}

	return nil
}

func (r *LoginPolicyRepo) Update(ctx context.Context, req *operatorV1.UpdateLoginPolicyRequest) error {
	if req == nil || req.Data == nil {
		return operatorV1.ErrorAdminBadRequest("invalid request")
	}
	if req.GetId() == 0 {
		return operatorV1.ErrorAdminBadRequest("id is required")
	}

	// 如果不存在则创建
	if req.GetAllowMissing() {
		exist, err := r.IsExist(ctx, req.GetId())
		if err != nil {
			return err
		}
		if !exist {
			createReq := &operatorV1.CreateLoginPolicyRequest{Data: req.Data}
			createReq.Data.CreatedBy = createReq.Data.UpdatedBy
			createReq.Data.UpdatedBy = nil
			return r.Create(ctx, createReq)
		}
	}

	builder := r.dbClient.Client().LoginPolicy.Update()
	err := r.repository.UpdateX(ctx, builder, req.Data, req.GetUpdateMask(),
		func(dto *operatorV1.LoginPolicy) {
			builder.
				SetNillableTargetID(req.Data.TargetId).
				SetNillableType(r.typeConverter.ToEntity(req.Data.Type)).
				SetNillableMethod(r.methodConverter.ToEntity(req.Data.Method)).
				SetNillableValue(req.Data.Value).
				SetNillableReason(req.Data.Reason).
				SetNillableUpdatedBy(req.Data.UpdatedBy).
				SetUpdatedAt(time.Now())
		},
		queryscope.Equal(loginpolicy.FieldID, req.GetId()),
	)

	return err
}

func (r *LoginPolicyRepo) Delete(ctx context.Context, req *operatorV1.DeleteLoginPolicyRequest) error {
	if req == nil {
		return operatorV1.ErrorAdminBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().LoginPolicy.Delete()
	_, err := r.repository.Delete(ctx, builder, queryscope.Equal(loginpolicy.FieldID, req.GetId()))
	if err != nil {
		r.log.Errorf(ctx, "delete internal message categories failed: %s", err.Error())
		return operatorV1.ErrorAdminInternalServerError("delete admin login restriction failed")
	}

	return nil
}
