package data

import (
	"context"
	"encoding/base64"

	"time"

	"github.com/hmlax/muskdeer/operator/internal/data/store/queryscope"

	"github.com/tx7do/go-crud/viewer"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"

	"github.com/tx7do/go-utils/copierutil"
	"github.com/tx7do/go-utils/crypto"
	"github.com/tx7do/go-utils/mapper"
	"github.com/tx7do/go-utils/password"
	"github.com/tx7do/go-utils/trans"

	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/predicate"

	"github.com/hmlax/muskdeer/operator/internal/data/store/usercredential"

	"github.com/hmlax/muskdeer/deer/management/constants"
	"github.com/hmlax/muskdeer/deer/management/runtimeconfig"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

const errorCredentialUserRequired = "user id is required"

type UserCredentialRepo struct {
	dbClient *store.DBClient
	log      *bLogger.Helper

	mapper                  *mapper.CopierMapper[operatorV1.UserCredential, store.UserCredential]
	statusConverter         *mapper.EnumTypeConverter[operatorV1.UserCredential_Status, usercredential.Status]
	identityTypeConverter   *mapper.EnumTypeConverter[operatorV1.UserCredential_IdentityType, usercredential.IdentityType]
	credentialTypeConverter *mapper.EnumTypeConverter[operatorV1.UserCredential_CredentialType, usercredential.CredentialType]

	passwordCrypto password.Crypto
	configRepo     *ConfigRepo
	credentialKey  []byte

	repository *store.Repository[operatorV1.UserCredential, store.UserCredential]
}

func NewUserCredentialRepo(ctx *bootstrap.Context, dbClient *store.DBClient, passwordCrypto password.Crypto, configRepo *ConfigRepo) *UserCredentialRepo {
	repo := &UserCredentialRepo{
		log:                     ctx.NewLoggerHelper("user-credentials/repo/admin-service"),
		dbClient:                dbClient,
		passwordCrypto:          passwordCrypto,
		configRepo:              configRepo,
		credentialKey:           []byte(runtimeconfig.SettingsFromContext(ctx).CryptoKey),
		mapper:                  mapper.NewCopierMapper[operatorV1.UserCredential, store.UserCredential](),
		statusConverter:         mapper.NewEnumTypeConverter[operatorV1.UserCredential_Status, usercredential.Status](operatorV1.UserCredential_Status_name, operatorV1.UserCredential_Status_value),
		identityTypeConverter:   mapper.NewEnumTypeConverter[operatorV1.UserCredential_IdentityType, usercredential.IdentityType](operatorV1.UserCredential_IdentityType_name, operatorV1.UserCredential_IdentityType_value),
		credentialTypeConverter: mapper.NewEnumTypeConverter[operatorV1.UserCredential_CredentialType, usercredential.CredentialType](operatorV1.UserCredential_CredentialType_name, operatorV1.UserCredential_CredentialType_value),
	}

	repo.init()

	return repo
}

func (r *UserCredentialRepo) init() {
	r.repository = store.NewRepository[operatorV1.UserCredential, store.UserCredential](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())

	r.mapper.AppendConverters(r.statusConverter.NewConverterPair())
	r.mapper.AppendConverters(r.identityTypeConverter.NewConverterPair())
	r.mapper.AppendConverters(r.credentialTypeConverter.NewConverterPair())
}

func (r *UserCredentialRepo) IsExist(ctx context.Context, id uint32) (bool, error) {
	exist, err := r.dbClient.Client().UserCredential.Query().
		Where(usercredential.IDEQ(id)).
		Exist(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query exist failed: %s", err.Error())
		return false, operatorV1.ErrorAuthenticationInternalServerError("query exist failed")
	}
	return exist, nil
}

func (r *UserCredentialRepo) Count(ctx context.Context, whereCond []queryscope.Scope) (int, error) {
	builder := r.dbClient.Client().UserCredential.Query()
	if len(whereCond) != 0 {
		builder.Modify(whereCond...)
	}

	count, err := builder.Count(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query count failed: %s", err.Error())
		return 0, operatorV1.ErrorAuthenticationInternalServerError("query count failed")
	}

	return count, nil
}

func (r *UserCredentialRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListUserCredentialResponse, error) {
	if req == nil {
		return nil, operatorV1.ErrorAuthenticationBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().UserCredential.Query()

	ret, err := r.repository.ListWithPaging(ctx, builder, builder.Clone(), req)
	if err != nil {
		return nil, err
	}
	if ret == nil {
		return &operatorV1.ListUserCredentialResponse{
			Total: 0,
			Items: nil,
		}, nil
	}

	return &operatorV1.ListUserCredentialResponse{
		Total: ret.Total,
		Items: ret.Items,
	}, nil
}

func (r *UserCredentialRepo) Create(ctx context.Context, req *operatorV1.CreateUserCredentialRequest) (err error) {
	if req == nil || req.Data == nil {
		return operatorV1.ErrorIdentityBadRequest("invalid parameter")
	}

	var tx *store.Tx
	tx, err = r.dbClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return operatorV1.ErrorIdentityInternalServerError("start transaction failed")
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

	return r.CreateWithTx(ctx, tx, req.GetData())
}

func (r *UserCredentialRepo) CreateWithTx(ctx context.Context, tx *store.Tx, data *operatorV1.UserCredential) error {
	if data == nil {
		return operatorV1.ErrorAuthenticationBadRequest("invalid request")
	}

	var err error

	if data.Credential != nil {
		var newCredential string
		newCredential, err = r.prepareCredential(ctx, r.credentialTypeConverter.ToEntity(data.CredentialType), data.GetCredential())
		if err != nil {
			// 口令策略（复杂度等）错误原样透传，便于前端给出可操作的提示
			r.log.Warnf(ctx, "prepare new credential rejected: %s", err.Error())
			return err
		}
		data.Credential = trans.Ptr(newCredential)
	}

	builder := tx.UserCredential.Create()
	builder.
		SetUserID(data.GetUserId()).
		SetNillableTenantID(data.TenantId).
		SetNillableIdentityType(r.identityTypeConverter.ToEntity(data.IdentityType)).
		SetNillableIdentifier(data.Identifier).
		SetNillableCredentialType(r.credentialTypeConverter.ToEntity(data.CredentialType)).
		SetNillableCredential(data.Credential).
		SetNillableIsPrimary(data.IsPrimary).
		SetNillableStatus(r.statusConverter.ToEntity(data.Status)).
		SetNillableExtraInfo(data.ExtraInfo).
		SetNillableProvider(data.Provider).
		SetNillableProviderAccountID(data.ProviderAccountId).
		SetCreatedAt(time.Now())

	if err = builder.Exec(ctx); err != nil {
		r.log.Errorf(ctx, "insert user credential failed: %s [%v]", err.Error(), data)
		return operatorV1.ErrorAuthenticationInternalServerError("insert user credential failed")
	}

	return nil
}

func (r *UserCredentialRepo) Update(ctx context.Context, req *operatorV1.UpdateUserCredentialRequest) error {
	if req == nil || req.Data == nil {
		return operatorV1.ErrorAuthenticationBadRequest("invalid request")
	}
	if req.GetId() == 0 {
		return operatorV1.ErrorAuthenticationBadRequest("id is required")
	}

	// 如果不存在则创建
	if req.GetAllowMissing() {
		exist, err := r.IsExist(ctx, req.GetId())
		if err != nil {
			return err
		}
		if !exist {
			err = r.Create(ctx, &operatorV1.CreateUserCredentialRequest{Data: req.Data})
			return err
		}
	}

	var err error

	if req.Data.Credential != nil {
		var newCredential string
		newCredential, err = r.prepareCredential(ctx, r.credentialTypeConverter.ToEntity(req.Data.CredentialType), req.Data.GetCredential())
		if err != nil {
			// 口令策略（复杂度等）错误原样透传，便于前端给出可操作的提示
			r.log.Warnf(ctx, "prepare new credential rejected: %s", err.Error())
			return err
		}
		req.Data.Credential = trans.Ptr(newCredential)
	}

	builder := r.dbClient.Client().UserCredential.Update()
	err = r.repository.UpdateX(ctx, builder, req.Data, req.GetUpdateMask(),
		func(dto *operatorV1.UserCredential) {
			builder.
				SetNillableIdentityType(r.identityTypeConverter.ToEntity(req.Data.IdentityType)).
				SetNillableIdentifier(req.Data.Identifier).
				SetNillableCredentialType(r.credentialTypeConverter.ToEntity(req.Data.CredentialType)).
				SetNillableCredential(req.Data.Credential).
				SetNillableIsPrimary(req.Data.IsPrimary).
				SetNillableStatus(r.statusConverter.ToEntity(req.Data.Status)).
				SetNillableExtraInfo(req.Data.ExtraInfo).
				SetNillableProvider(req.Data.Provider).
				SetNillableProviderAccountID(req.Data.ProviderAccountId).
				SetUpdatedAt(time.Now())
		},
		queryscope.Equal(usercredential.FieldID, req.GetId()),
	)

	return err
}

func (r *UserCredentialRepo) Delete(ctx context.Context, id uint32) error {
	builder := r.dbClient.Client().UserCredential.Delete()
	builder.Where(usercredential.IDEQ(id))
	if affected, err := builder.Exec(ctx); err != nil {
		if store.IsNotFound(err) {
			return operatorV1.ErrorAuthenticationNotFound("user credential not found")
		}

		r.log.Errorf(ctx, "delete one data failed: %s", err.Error())

		return operatorV1.ErrorAuthenticationInternalServerError("delete one data failed")
	} else {
		if affected == 0 {
			return operatorV1.ErrorAuthenticationNotFound("user credential not found")
		} else {
			return nil
		}
	}
}

// DeleteByUserId deletes credentials belonging to the specified user.
func (r *UserCredentialRepo) DeleteByUserId(ctx context.Context, userId uint32) error {
	builder := r.dbClient.Client().UserCredential.Delete()
	builder.Where(usercredential.UserIDEQ(userId))
	if affected, err := builder.Exec(ctx); err != nil {
		if store.IsNotFound(err) {
			return operatorV1.ErrorAuthenticationNotFound("user credential not found")
		}

		r.log.Errorf(ctx, "delete one data failed: %s", err.Error())

		return operatorV1.ErrorAuthenticationInternalServerError("delete one data failed")
	} else {
		if affected == 0 {
			return operatorV1.ErrorAuthenticationNotFound("user credential not found")
		} else {
			return nil
		}
	}
}

func (r *UserCredentialRepo) DeleteByIdentifier(ctx context.Context, identityType operatorV1.UserCredential_IdentityType, identifier string) error {
	builder := r.dbClient.Client().UserCredential.Delete()
	builder.Where(
		usercredential.IdentityTypeEQ(*r.identityTypeConverter.ToEntity(&identityType)),
		usercredential.IdentifierEQ(identifier),
	)
	// 租户范围过滤（平台管理员 tenant_id=0 不限定）
	if tid, hasTenant := maybeTenantFromViewer(ctx); hasTenant {
		builder.Where(usercredential.TenantIDEQ(tid))
	}
	if affected, err := builder.Exec(ctx); err != nil {
		if store.IsNotFound(err) {
			return operatorV1.ErrorAuthenticationNotFound("user credential not found")
		}

		r.log.Errorf(ctx, "delete one data failed: %s", err.Error())

		return operatorV1.ErrorAuthenticationInternalServerError("delete one data failed")
	} else {
		if affected == 0 {
			return operatorV1.ErrorAuthenticationNotFound("user credential not found")
		} else {
			return nil
		}
	}
}

func (r *UserCredentialRepo) Get(ctx context.Context, req *operatorV1.GetUserCredentialRequest) (*operatorV1.UserCredential, error) {
	if req == nil {
		return nil, operatorV1.ErrorAuthenticationBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().UserCredential.Query()

	var whereCond []queryscope.Scope
	switch req.QueryBy.(type) {
	default:
	case *operatorV1.GetUserCredentialRequest_Id:
		whereCond = append(whereCond, usercredential.IDEQ(req.GetId()))
	}

	dto, err := r.repository.Get(ctx, builder, req.GetViewMask(), whereCond...)
	if err != nil {
		return nil, err
	}

	return dto, err
}

// maybeTenantFromViewer returns the viewer's tenant id if present and >0, plus whether a viewer context exists.
func maybeTenantFromViewer(ctx context.Context) (tenantID uint32, hasTenant bool) {
	vc, exist := viewer.FromContext(ctx)
	if !exist {
		return 0, false
	}
	tid := vc.TenantID()
	if tid == 0 {
		return 0, false
	}
	return uint32(tid), true
}

func (r *UserCredentialRepo) GetByIdentifier(ctx context.Context, req *operatorV1.GetUserCredentialByIdentifierRequest) (*operatorV1.UserCredential, error) {
	builder := r.dbClient.Client().UserCredential.Query()

	var whereConds []predicate.UserCredential
	whereConds = append(whereConds,
		usercredential.IdentityTypeEQ(*r.identityTypeConverter.ToEntity(trans.Ptr(req.GetIdentityType()))),
		usercredential.IdentifierEQ(req.GetIdentifier()),
	)

	// 租户范围过滤（平台管理员 tenant_id=0 不限定）
	if req.TenantId != nil {
		whereConds = append(whereConds, usercredential.TenantIDEQ(req.GetTenantId()))
	} else if tid, hasTenant := maybeTenantFromViewer(ctx); hasTenant {
		whereConds = append(whereConds, usercredential.TenantIDEQ(tid))
	}

	builder.Where(whereConds...)

	entity, err := builder.Only(ctx)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, operatorV1.ErrorAuthenticationNotFound("user credential not found")
		}

		r.log.Errorf(ctx, "query one data failed: %s", err.Error())

		return nil, operatorV1.ErrorAuthenticationInternalServerError("query data failed")
	}

	return r.mapper.ToDTO(entity), nil
}

// dummyPasswordHash 是一个合法的 bcrypt 哈希，用于在用户不存在时执行一次假校验，
// 让"用户不存在"与"密码错误"两条路径耗时一致，抹掉用户名枚举的计时侧信道。
// 明文不可逆，仅作为恒定耗时的目标。
const dummyPasswordHash = "$2a$10$1sbpKmhQDpXLHnDnEQ1nLe3oOnYyP2bUJyqHcX2T0Fq1qfyoXOrPm"

// credentialAESKey uses the deployment key. Runtime configuration requires it;
// direct repository test fixtures without a bootstrap context retain the default.
func (r *UserCredentialRepo) credentialAESKey() []byte {
	if len(r.credentialKey) > 0 {
		return r.credentialKey
	}
	return crypto.DefaultAESKey
}

// performDummyVerify 执行一次假的密码校验，用于抹平计时差异。
// 返回值始终为 false，且从不抛错。
func (r *UserCredentialRepo) performDummyVerify(plainCredential string) {
	_, _ = r.passwordCrypto.Verify(plainCredential, dummyPasswordHash)
}

// FindUserCredential 在指定租户范围内根据身份类型+标识符查询单条凭证并校验密码。
// tenantID 由调用方解析确定（登录时按 tenant_code 解析；留空视为平台 tenant 0）。
// (tenant_id, identity_type, identifier) 在该 tenant 内唯一，无需跨租户消歧。
// needDecrypt 为 true 时，plainCredential 应为 base64+AES 加密密文，会自动解密。
// 返回匹配到的用户 ID，调用方可据此通过 ID 查找用户。
func (r *UserCredentialRepo) FindUserCredential(ctx context.Context, tenantID uint32, identityType operatorV1.UserCredential_IdentityType, identifier, plainCredential string, needDecrypt bool) (uint32, error) {
	if needDecrypt {
		bytesPass, err := base64.StdEncoding.DecodeString(plainCredential)
		if err != nil {
			r.log.Errorf(ctx, "decode base64 credential failed: %s", err.Error())
			return 0, operatorV1.ErrorAuthenticationBadRequest("invalid credential format")
		}
		decrypted, err := crypto.AesDecrypt(bytesPass, r.credentialAESKey(), nil)
		if err != nil {
			r.log.Errorf(ctx, "decrypt credential failed: %s", err.Error())
			return 0, operatorV1.ErrorAuthenticationBadRequest("decrypt credential failed")
		}
		plainCredential = string(decrypted)
	}

	entity, err := r.dbClient.Client().UserCredential.Query().
		Select(
			usercredential.FieldUserID,
			usercredential.FieldCredentialType,
			usercredential.FieldCredential,
			usercredential.FieldStatus,
			usercredential.FieldUpdatedAt,
			usercredential.FieldExtraInfo,
		).
		Where(
			usercredential.TenantIDEQ(tenantID),
			usercredential.IdentityTypeEQ(*r.identityTypeConverter.ToEntity(trans.Ptr(identityType))),
			usercredential.IdentifierEQ(identifier),
		).
		Only(ctx)
	if err != nil {
		// 恒定时间防护：用户不存在时也跑一次 bcrypt 校验，避免被计时攻击枚举用户名
		r.performDummyVerify(plainCredential)
		if store.IsNotFound(err) {
			return 0, operatorV1.ErrorAuthenticationUserNotFound("user not found")
		}
		r.log.Errorf(ctx, "query credential failed: %s", err.Error())
		return 0, operatorV1.ErrorAuthenticationServiceUnavailable("db error")
	}

	if entity.Status == nil || entity.CredentialType == nil || entity.Credential == nil || entity.UserID == nil {
		r.performDummyVerify(plainCredential)
		return 0, operatorV1.ErrorAuthenticationUserNotFound("user not found")
	}
	if *entity.Status != usercredential.StatusEnabled {
		r.performDummyVerify(plainCredential)
		return 0, operatorV1.ErrorAuthenticationUserNotFound("user not found")
	}

	if r.verifyCredential(entity.CredentialType, plainCredential, *entity.Credential) {
		// 等保口令策略：有效期检查——超期拒绝登录，用户走重置/改密流程换新口令
		// 阈值自 sys_config 平台参数读取（参数管理页可调，<=0 关闭）。
		if maxAge := r.configRepo.GetConfigInt(ctx, constants.ConfigKeyPasswordMaxAgeDays, constants.DefaultPasswordMaxAgeDays); maxAge > 0 && *entity.CredentialType == usercredential.CredentialTypePasswordHash {
			if entity.UpdatedAt != nil && time.Since(*entity.UpdatedAt) > time.Duration(maxAge)*24*time.Hour {
				r.log.Warnf(ctx, "password expired for user [%d] (age > %dd), login denied", *entity.UserID, maxAge)
				return 0, operatorV1.ErrorAuthenticationBadRequest("password expired, please reset your password")
			}
		}
		return *entity.UserID, nil
	}

	return 0, operatorV1.ErrorAuthenticationInvalidPassword("incorrect password")
}

func (r *UserCredentialRepo) VerifyCredential(ctx context.Context, req *operatorV1.VerifyCredentialRequest) (*operatorV1.VerifyCredentialResponse, error) {
	// 该内部 RPC 请求未携带租户信息，按平台（tenant 0）范围校验。
	if _, err := r.FindUserCredential(ctx, 0, req.GetIdentityType(), req.GetIdentifier(), req.GetCredential(), req.GetNeedDecrypt()); err != nil {
		return nil, err
	}

	return &operatorV1.VerifyCredentialResponse{
		Success: true,
	}, nil
}

func (r *UserCredentialRepo) verifyCredential(credentialType *usercredential.CredentialType, plainCredential, targetCredential string) bool {
	if credentialType == nil || plainCredential == "" {
		return false
	}

	switch *credentialType {
	case usercredential.CredentialTypePasswordHash:
		ok, err := r.passwordCrypto.Verify(plainCredential, targetCredential)
		if err != nil {
			r.log.Errorf(context.Background(), "verify password failed: %s", err.Error())
			return false
		}
		return ok
	default:
		return plainCredential == targetCredential
	}
}

func (r *UserCredentialRepo) prepareCredential(ctx context.Context, credentialType *usercredential.CredentialType, plainCredential string) (string, error) {
	var newCredential string
	switch *credentialType {
	case usercredential.CredentialTypePasswordHash:
		// 等保口令策略：哈希前对明文做复杂度校验（覆盖创建/修改/重置全部路径）。
		// 长度阈值自 sys_config 平台参数读取（参数管理页可调）。
		if err := password.ValidateComplexity(plainCredential,
			r.configRepo.GetConfigInt(ctx, constants.ConfigKeyPasswordMinLen, constants.DefaultPasswordMinLen)); err != nil {
			return "", operatorV1.ErrorAuthenticationBadRequest("%s", err.Error())
		}
		var err error
		// 加密明文密码
		newCredential, err = r.passwordCrypto.Encrypt(plainCredential)
		if err != nil {
			r.log.Errorf(ctx, "hash new password failed: %s", err.Error())
			return "", operatorV1.ErrorAuthenticationBadRequest("hash new password failed")
		}

	default:
		newCredential = plainCredential
	}

	return newCredential, nil
}

// ChangeCredential 修改认证信息
func (r *UserCredentialRepo) ChangeCredential(ctx context.Context, req *operatorV1.ChangeCredentialRequest) error {
	if req.GetNeedDecrypt() {
		// 解密旧密码
		oldBytes, err := base64.StdEncoding.DecodeString(req.GetOldCredential())
		if err != nil {
			r.log.Errorf(ctx, "decode base64 old credential failed: %s", err.Error())
			return operatorV1.ErrorAuthenticationBadRequest("invalid old credential format")
		}
		oldPlain, err := crypto.AesDecrypt(oldBytes, r.credentialAESKey(), nil)
		if err != nil {
			r.log.Errorf(ctx, "decrypt old credential failed: %s", err.Error())
			return operatorV1.ErrorAuthenticationBadRequest("decrypt old credential failed")
		}
		req.OldCredential = string(oldPlain)

		// 解密新密码。此前 base64/AES 错误被吞掉（_），解密失败会把空串/垃圾串当成新口令
		// 哈希存储，导致后续登录恒失败且难以排查。这里与 VerifyCredential 对齐：解密失败即报错。
		newBytes, err := base64.StdEncoding.DecodeString(req.GetNewCredential())
		if err != nil {
			r.log.Errorf(ctx, "decode base64 new credential failed: %s", err.Error())
			return operatorV1.ErrorAuthenticationBadRequest("invalid new credential format")
		}
		newPlain, err := crypto.AesDecrypt(newBytes, r.credentialAESKey(), nil)
		if err != nil {
			r.log.Errorf(ctx, "decrypt new credential failed: %s", err.Error())
			return operatorV1.ErrorAuthenticationBadRequest("decrypt new credential failed")
		}
		req.NewCredential = string(newPlain)
	}

	// 租户范围 where 条件（平台管理员 tenant_id=0 不限定）
	tenantWhere := []predicate.UserCredential{
		usercredential.IdentityTypeEQ(*r.identityTypeConverter.ToEntity(trans.Ptr(req.GetIdentityType()))),
		usercredential.IdentifierEQ(req.GetIdentifier()),
	}
	if tid, hasTenant := maybeTenantFromViewer(ctx); hasTenant {
		tenantWhere = append(tenantWhere, usercredential.TenantIDEQ(tid))
	}

	entity, err := r.dbClient.Client().UserCredential.
		Query().
		Select(
			usercredential.FieldCredentialType,
			usercredential.FieldCredential,
			usercredential.FieldExtraInfo,
		).
		Where(tenantWhere...).
		Only(ctx)
	if err != nil {
		if store.IsNotFound(err) {
			return operatorV1.ErrorAuthenticationNotFound("user credential not found")
		}
		r.log.Errorf(ctx, "query one data failed: %s", err.Error())
		return operatorV1.ErrorAuthenticationInternalServerError("query one data failed")
	}

	if entity.CredentialType == nil {
		return operatorV1.ErrorAuthenticationNotFound("user credential not found")
	}

	// 验证旧认证信息
	if !r.verifyCredential(entity.CredentialType, req.GetOldCredential(), *entity.Credential) {
		return operatorV1.ErrorAuthenticationBadRequest("invalid old password")
	}

	var newCredential string
	newCredential, err = r.prepareCredential(ctx, entity.CredentialType, req.GetNewCredential())
	if err != nil {
		// 口令策略（复杂度等）错误原样透传，便于前端给出可操作的提示
		r.log.Warnf(ctx, "prepare new credential rejected: %s", err.Error())
		return err
	}

	if newCredential == "" {
		return operatorV1.ErrorAuthenticationBadRequest("new credential cannot be empty")
	}

	// 等保口令策略：历史口令检查——新明文不得与最近 N 条历史哈希重复
	if err := r.checkPasswordHistory(ctx, entity, req.GetNewCredential()); err != nil {
		return err
	}

	extraInfo := appendPasswordHistory(entity.ExtraInfo, *entity.Credential,
		r.configRepo.GetConfigInt(ctx, constants.ConfigKeyPasswordHistoryCount, constants.DefaultPasswordHistoryCount))

	builder := r.dbClient.Client().UserCredential.Update()
	builder.Where(tenantWhere...)
	builder.
		SetCredential(newCredential).
		SetNillableExtraInfo(extraInfo).
		SetUpdatedAt(time.Now())
	if err = builder.Exec(ctx); err != nil {
		r.log.Errorf(ctx, "update one data failed: %s", err.Error())
		return operatorV1.ErrorAuthenticationInternalServerError("update data failed")
	}

	return nil
}

// ResetCredential 修改认证信息
func (r *UserCredentialRepo) ResetCredential(ctx context.Context, req *operatorV1.ResetCredentialRequest) error {
	return r.resetCredential(ctx, req, nil)
}

// ResetCredentialForUser targets an already authorized user without relying on
// username uniqueness across tenants. The caller's tenant privacy still applies.
func (r *UserCredentialRepo) ResetCredentialForUser(ctx context.Context, tenantID, userID uint32, req *operatorV1.ResetCredentialRequest) error {
	if userID == 0 {
		return operatorV1.ErrorAuthenticationBadRequest(errorCredentialUserRequired)
	}
	where := []predicate.UserCredential{usercredential.UserIDEQ(userID)}
	if tenantID == 0 {
		// Legacy platform credentials can store the platform tenant as NULL.
		where = append(where, usercredential.Or(usercredential.TenantIDEQ(0), usercredential.TenantIDIsNil()))
	} else {
		where = append(where, usercredential.TenantIDEQ(tenantID))
	}
	return r.resetCredential(ctx, req, where)
}

func (r *UserCredentialRepo) resetCredential(ctx context.Context, req *operatorV1.ResetCredentialRequest, targetWhere []predicate.UserCredential) error {
	if req.GetNeedDecrypt() {
		// 解密新密码（base64/AES 错误不可吞：吞错会把空串/垃圾串哈希存为新口令）
		bytesPass, err := base64.StdEncoding.DecodeString(req.GetNewCredential())
		if err != nil {
			r.log.Errorf(ctx, "decode base64 new credential failed: %s", err.Error())
			return operatorV1.ErrorAuthenticationBadRequest("invalid new credential format")
		}
		plainPassword, err := crypto.AesDecrypt(bytesPass, r.credentialAESKey(), nil)
		if err != nil {
			r.log.Errorf(ctx, "decrypt new credential failed: %s", err.Error())
			return operatorV1.ErrorAuthenticationBadRequest("decrypt new credential failed")
		}
		req.NewCredential = string(plainPassword)
	}

	// 租户范围 where 条件（平台管理员 tenant_id=0 不限定）
	tenantWhere := []predicate.UserCredential{
		usercredential.IdentityTypeEQ(*r.identityTypeConverter.ToEntity(trans.Ptr(req.GetIdentityType()))),
		usercredential.IdentifierEQ(req.GetIdentifier()),
	}
	tenantWhere = append(tenantWhere, targetWhere...)
	if tid, hasTenant := maybeTenantFromViewer(ctx); hasTenant {
		tenantWhere = append(tenantWhere, usercredential.TenantIDEQ(tid))
	}

	entity, err := r.dbClient.Client().UserCredential.
		Query().
		Select(
			usercredential.FieldCredentialType,
			usercredential.FieldCredential,
			usercredential.FieldExtraInfo,
		).
		Where(tenantWhere...).
		Only(ctx)
	if err != nil {
		if store.IsNotFound(err) {
			return operatorV1.ErrorAuthenticationNotFound("user credential not found")
		}
		r.log.Errorf(ctx, "query one data failed: %s", err.Error())
		return operatorV1.ErrorAuthenticationInternalServerError("query one data failed")
	}

	if entity.CredentialType == nil {
		return operatorV1.ErrorAuthenticationNotFound("user credential not found")
	}

	var newCredential string
	newCredential, err = r.prepareCredential(ctx, entity.CredentialType, req.GetNewCredential())
	if err != nil {
		// 口令策略（复杂度等）错误原样透传，便于前端给出可操作的提示
		r.log.Warnf(ctx, "prepare new credential rejected: %s", err.Error())
		return err
	}

	if newCredential == "" {
		return operatorV1.ErrorAuthenticationBadRequest("new credential cannot be empty")
	}

	// 等保口令策略：历史口令检查（管理员重置同样不得与近期历史重复）
	if err := r.checkPasswordHistory(ctx, entity, req.GetNewCredential()); err != nil {
		return err
	}

	extraInfo := appendPasswordHistory(entity.ExtraInfo, *entity.Credential,
		r.configRepo.GetConfigInt(ctx, constants.ConfigKeyPasswordHistoryCount, constants.DefaultPasswordHistoryCount))

	builder := r.dbClient.Client().UserCredential.Update()
	builder.Where(tenantWhere...)
	builder.
		SetCredential(newCredential).
		SetNillableExtraInfo(extraInfo).
		SetUpdatedAt(time.Now())
	if err = builder.Exec(ctx); err != nil {
		r.log.Errorf(ctx, "update one data failed: %s", err.Error())
		return operatorV1.ErrorAuthenticationInternalServerError("update data failed")
	}

	return nil
}
