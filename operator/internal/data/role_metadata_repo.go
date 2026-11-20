package data

import (
	"context"
	"time"

	"github.com/tx7do/go-utils/copierutil"
	"github.com/tx7do/go-utils/mapper"
	"github.com/tx7do/go-utils/timeutil"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/rolemetadata"
)

type RoleMetadataRepo struct {
	dbClient *store.DBClient
	log      *bLogger.Helper

	mapper              *mapper.CopierMapper[operatorV1.RoleMetadata, store.RoleMetadata]
	syncPolicyConverter *mapper.EnumTypeConverter[operatorV1.RoleMetadata_SyncPolicy, rolemetadata.SyncPolicy]
	scopeConverter      *mapper.EnumTypeConverter[operatorV1.RoleMetadata_Scope, rolemetadata.Scope]

	repository *store.Repository[operatorV1.RoleMetadata, store.RoleMetadata]
}

func NewRoleMetadataRepo(
	ctx *bootstrap.Context,
	dbClient *store.DBClient,
) *RoleMetadataRepo {
	repo := &RoleMetadataRepo{
		log:      ctx.NewLoggerHelper("role-metadata/repo/admin-service"),
		dbClient: dbClient,
		mapper:   mapper.NewCopierMapper[operatorV1.RoleMetadata, store.RoleMetadata](),
		syncPolicyConverter: mapper.NewEnumTypeConverter[operatorV1.RoleMetadata_SyncPolicy, rolemetadata.SyncPolicy](
			operatorV1.RoleMetadata_SyncPolicy_name, operatorV1.RoleMetadata_SyncPolicy_value,
		),
		scopeConverter: mapper.NewEnumTypeConverter[operatorV1.RoleMetadata_Scope, rolemetadata.Scope](
			operatorV1.RoleMetadata_Scope_name, operatorV1.RoleMetadata_Scope_value,
		),
	}

	repo.init()

	return repo
}

func (r *RoleMetadataRepo) init() {
	r.repository = store.NewRepository[operatorV1.RoleMetadata, store.RoleMetadata](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())

	r.mapper.AppendConverters(r.syncPolicyConverter.NewConverterPair())
	r.mapper.AppendConverters(r.scopeConverter.NewConverterPair())
}

func (r *RoleMetadataRepo) Create(ctx context.Context, tx *store.Tx, data *operatorV1.RoleMetadata) error {
	err := tx.RoleMetadata.Create().
		SetNillableTenantID(data.TenantId).
		SetRoleID(data.GetRoleId()).
		SetNillableIsTemplate(data.IsTemplate).
		SetNillableTemplateFor(data.TemplateFor).
		SetNillableTemplateVersion(data.TemplateVersion).
		SetNillableLastSyncedVersion(data.LastSyncedVersion).
		SetNillableLastSyncedAt(timeutil.TimestamppbToTime(data.LastSyncedAt)).
		SetNillableSyncPolicy(r.syncPolicyConverter.ToEntity(data.SyncPolicy)).
		SetNillableScope(r.scopeConverter.ToEntity(data.Scope)).
		SetCustomOverrides(roleOverridesOrEmpty(data.CustomOverrides)).
		SetCreatedAt(time.Now()).
		SetNillableCreatedBy(data.CreatedBy).
		Exec(ctx)
	return err
}

// Upsert 插入或更新角色元数据
func (r *RoleMetadataRepo) Upsert(ctx context.Context, data *operatorV1.RoleMetadata) error {
	now := time.Now()
	builder := r.dbClient.Client().RoleMetadata.Create().
		SetNillableTenantID(data.TenantId).
		SetRoleID(data.GetRoleId()).
		SetNillableIsTemplate(data.IsTemplate).
		SetNillableTemplateFor(data.TemplateFor).
		SetNillableTemplateVersion(data.TemplateVersion).
		SetNillableLastSyncedVersion(data.LastSyncedVersion).
		SetNillableLastSyncedAt(timeutil.TimestamppbToTime(data.LastSyncedAt)).
		SetNillableSyncPolicy(r.syncPolicyConverter.ToEntity(data.SyncPolicy)).
		SetNillableScope(r.scopeConverter.ToEntity(data.Scope)).
		SetCustomOverrides(roleOverridesOrEmpty(data.CustomOverrides)).
		SetCreatedAt(now).
		SetNillableCreatedBy(data.CreatedBy).
		// 冲突目标必须与 schema 唯一索引 idx_role_metadata_tenant_role
		// (tenant_id, role_id) 的复合列精确一致，只写 role_id 会被
		// SQLite/PG 以 "ON CONFLICT clause does not match any UNIQUE
		// constraint" 整条拒绝，Upsert 从未生效过。
		OnConflictColumns(
			rolemetadata.FieldTenantID,
			rolemetadata.FieldRoleID,
		).
		AddTemplateVersion(1).
		SetUpdatedAt(now).
		SetUpdatedBy(data.GetUpdatedBy())

	if data.LastSyncedAt != nil {
		builder.SetLastSyncedAt(*timeutil.TimestamppbToTime(data.LastSyncedAt))
	}
	if data.SyncPolicy != nil {
		builder.SetSyncPolicy(*r.syncPolicyConverter.ToEntity(data.SyncPolicy))
	}
	if data.Scope != nil {
		builder.SetScope(*r.scopeConverter.ToEntity(data.Scope))
	}
	if data.CustomOverrides != nil {
		builder.SetCustomOverrides(data.GetCustomOverrides())
	}

	err := builder.Exec(ctx)
	return err
}

// Role metadata requires a JSON object. GORM serializes a nil NOT NULL JSON
// field as an empty string, which PostgreSQL rejects instead of storing {}.
func roleOverridesOrEmpty(overrides *operatorV1.RoleOverride) *operatorV1.RoleOverride {
	if overrides == nil {
		return &operatorV1.RoleOverride{}
	}
	return overrides
}

// Get 获取角色元数据
func (r *RoleMetadataRepo) Get(ctx context.Context, roleID uint32) (*operatorV1.RoleMetadata, error) {
	rm, err := r.dbClient.Client().RoleMetadata.Query().
		Where(
			rolemetadata.RoleIDEQ(roleID),
		).
		Only(ctx)
	if err != nil {
		return nil, err
	}

	dto := r.mapper.ToDTO(rm)
	return dto, nil
}

// IsExistByRoleID 判断角色元数据是否存在
func (r *RoleMetadataRepo) IsExistByRoleID(ctx context.Context, roleID uint32) (bool, error) {
	count, err := r.dbClient.Client().RoleMetadata.Query().
		Where(
			rolemetadata.RoleIDEQ(roleID),
		).
		Count(ctx)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// IsTemplateRole 判断角色是否是模版角色
func (r *RoleMetadataRepo) IsTemplateRole(ctx context.Context, roleID uint32) (bool, error) {
	rm, err := r.dbClient.Client().RoleMetadata.Query().
		Where(
			rolemetadata.RoleIDEQ(roleID),
		).
		Only(ctx)
	if err != nil {
		return false, err
	}
	var isTemplate bool
	if rm.IsTemplate != nil {
		isTemplate = *rm.IsTemplate
	}
	return isTemplate, nil
}

// UpgradeTemplateVersion 升级模版版本号
// CleanByRoleID 在事务内清理指定角色的元数据行。
// 角色删除时随主记录一并清理，防止 sys_role_metadata 留孤儿行。
func (r *RoleMetadataRepo) CleanByRoleID(ctx context.Context, tx *store.Tx, roleID uint32) error {
	_, err := tx.RoleMetadata.Delete().
		Where(rolemetadata.RoleIDEQ(roleID)).
		Exec(ctx)
	return err
}

func (r *RoleMetadataRepo) UpgradeTemplateVersion(ctx context.Context, tx *store.Tx, roleID uint32) error {
	err := tx.RoleMetadata.Update().
		Where(
			rolemetadata.RoleIDEQ(roleID),
			rolemetadata.IsTemplateEQ(true),
		).
		AddTemplateVersion(1).
		SetUpdatedAt(time.Now()).
		Exec(ctx)
	if err != nil {
		return err
	}

	return err
}
