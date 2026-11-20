// RoleService 的 SQLite 内存库集成测试（白盒，包内测试）。
//
// 覆盖目标：
//   - List / Get 的 enrichment：TenantName 经 tenantRepo.ListTenantsByIds 从租户表回填；
//     平台级角色（TenantId 未设置）不回填。
//   - Create / Delete 基本路径：Create 走 auth.FromContext 操作人注入（CreatedBy 回填）
//     与默认角色播种；Delete 走非保护角色删除 + 关联清理。
//
// 桩说明：Create/Update/Delete 尾部会调 authorizer.ResetPolicies，nil 会 panic，
// 按 NewAuthorizer 的 noop 引擎形态构造最小可用 authorizer（Provider 桩返回空策略，
// noop 引擎的 ResetPolicies 直接返回 nil）。
package service

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	"github.com/tx7do/go-crud/viewer"
	"github.com/tx7do/go-utils/trans"
	conf "github.com/tx7do/kratos-bootstrap/api/gen/go/conf/v1"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	"github.com/hmlax/muskdeer/operator/internal/data"
	"github.com/hmlax/muskdeer/operator/internal/data/store"
	storetest "github.com/hmlax/muskdeer/operator/internal/data/storetest"

	"github.com/hmlax/muskdeer/deer/management/middleware/auth"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"

	"github.com/tx7do/go-utils/authorizer"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func TestRoleServiceTenantCannotGrantUnownedPermissions(t *testing.T) {
	svc, client := newRoleServiceFixture(t)
	sysCtx := storetest.NewSystemContext(context.Background())
	owned, err := client.Client().Permission.Create().SetCode("qa:owned").SetName("Owned").Save(sysCtx)
	require.NoError(t, err)
	foreign, err := client.Client().Permission.Create().SetCode("qa:platform").SetName("Platform").Save(sysCtx)
	require.NoError(t, err)
	const tenantID uint32 = 35
	const callerRoleCode = "tenant:grant-test"
	require.NoError(t, svc.roleRepo.Create(sysCtx, &operatorV1.CreateRoleRequest{
		Data: &operatorV1.Role{
			Name:        trans.Ptr("Grant test caller"),
			Code:        trans.Ptr(callerRoleCode),
			TenantId:    trans.Ptr(tenantID),
			Type:        operatorV1.Role_TENANT.Enum(),
			Status:      operatorV1.Role_ON.Enum(),
			Permissions: []uint32{owned.ID},
		},
	}))
	ctx := viewer.WithContext(context.Background(), viewer.NewUserContext(7, uint64(tenantID), 0, "test", []viewer.DataScope{{ScopeType: viewer.ScopeTypeAll}}))
	ctx = auth.NewContext(ctx, &operatorV1.UserTokenPayload{
		UserId:   7,
		TenantId: trans.Ptr(tenantID),
		Roles:    []string{callerRoleCode},
	})
	_, err = svc.Create(ctx, &operatorV1.CreateRoleRequest{
		Data: &operatorV1.Role{
			Name:        trans.Ptr("Denied role"),
			Code:        trans.Ptr("tenant:denied"),
			Permissions: []uint32{foreign.ID},
		},
	})
	require.True(t, operatorV1.IsPermissionForbidden(err))
	_, err = svc.Create(ctx, &operatorV1.CreateRoleRequest{
		Data: &operatorV1.Role{
			Name:        trans.Ptr("Allowed role"),
			Code:        trans.Ptr("tenant:allowed"),
			Permissions: []uint32{owned.ID},
		},
	})
	require.NoError(t, err)
	row, err := svc.roleRepo.Get(ctx, &operatorV1.GetRoleRequest{
		QueryBy: &operatorV1.GetRoleRequest_Code{Code: "tenant:allowed"},
	})
	require.NoError(t, err)
	_, err = svc.Update(ctx, &operatorV1.UpdateRoleRequest{
		Id:         row.GetId(),
		Data:       &operatorV1.Role{Permissions: []uint32{foreign.ID}},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"permissions"}},
	})
	require.True(t, operatorV1.IsPermissionForbidden(err))
	after, err := svc.roleRepo.Get(ctx, &operatorV1.GetRoleRequest{
		QueryBy: &operatorV1.GetRoleRequest_Id{Id: row.GetId()},
	})
	require.NoError(t, err)
	require.Equal(t, []uint32{owned.ID}, after.GetPermissions())
}

// roleServiceAuthProviderStub：authorizer.Provider 桩，返回空模型/空策略，
// 仅供 noop 引擎的 ResetPolicies 走通（noop 分支不消费策略数据）。
type roleServiceAuthProviderStub struct{}

func (roleServiceAuthProviderStub) ProvideModels(string) authorizer.ModelDataMap { return nil }

func (roleServiceAuthProviderStub) ProvidePolicies(context.Context) (authorizer.PermissionDataMap, error) {
	return nil, nil
}

// newRoleServiceForTest 白盒复刻 NewRoleService 的字段初始化：
// log 换 NopLogger，repo 用 testkit 构造器，authorizer 用 noop 引擎最小桩；
// 并复刻生产构造器的 svc.init()（空表时播种 constants.DefaultRoles）。
func newRoleServiceForTest(t *testing.T) *RoleService {
	t.Helper()
	svc, _ := newRoleServiceFixture(t)
	return svc
}

func newRoleServiceFixture(t *testing.T) (*RoleService, *store.DBClient) {
	t.Helper()
	dbClient := storetest.NewClientForTest(t)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, rdb.Close()) })
	authenticator, err := data.NewAuthenticatorForTest(&conf.Authentication_Jwt{
		Method: "HS256",
		Key:    authSvcTestJWTKey,
	}, data.NewUserTokenCacheForTest(rdb))
	require.NoError(t, err)
	// noop 引擎形态：EngineConfig 空类型按 noop 分支处理，日志用 NopLogger。
	svc := &RoleService{
		log:           bLogger.NewHelper(bLogger.NopLogger()),
		authorizer:    authorizer.NewAuthorizer(context.Background(), bLogger.NopLogger(), &authorizer.EngineConfig{}, roleServiceAuthProviderStub{}),
		roleRepo:      data.NewRoleRepoForTest(dbClient),
		tenantRepo:    data.NewTenantRepoForTest(dbClient),
		userRoleRepo:  data.NewUserRoleRepoForTest(dbClient),
		authenticator: authenticator,
	}
	svc.init()
	return svc, dbClient
}

// TestRoleServiceSqlite_ListEnrichment 验证 List 的 TenantName 回填：
// 租户级角色回填租户名，平台级角色（默认播种）不回填。
func TestRoleServiceSqlite_ListEnrichment(t *testing.T) {
	svc := newRoleServiceForTest(t)
	ctx := storetest.NewSystemContext(context.Background())

	tenant, err := svc.tenantRepo.Create(ctx, &operatorV1.Tenant{
		Name:        trans.Ptr("RoleSvc 富集租户甲"),
		Code:        trans.Ptr("ROLESVC_TENANT_A"),
		Status:      operatorV1.Tenant_ON.Enum(),
		Type:        operatorV1.Tenant_TRIAL.Enum(),
		AuditStatus: operatorV1.Tenant_APPROVED.Enum(),
	})
	require.NoError(t, err)
	require.NotNil(t, tenant.Id)

	err = svc.roleRepo.Create(ctx, &operatorV1.CreateRoleRequest{
		Data: &operatorV1.Role{
			TenantId: tenant.Id,
			Name:     trans.Ptr("RoleSvc 租户角色甲"),
			Code:     trans.Ptr("ROLESVC_ROLE_TENANT_A"),
			Status:   operatorV1.Role_ON.Enum(),
			Type:     operatorV1.Role_TENANT.Enum(),
		},
	})
	require.NoError(t, err)

	resp, err := svc.List(ctx, &paginationV1.PagingRequest{})
	require.NoError(t, err)

	var sawTenantRole bool
	var sawPlatformRoleWithoutTenant bool
	for _, item := range resp.GetItems() {
		if item.GetCode() == "ROLESVC_ROLE_TENANT_A" {
			sawTenantRole = true
			require.Equal(t, "RoleSvc 富集租户甲", item.GetTenantName(),
				"租户级角色应回填租户名")
			continue
		}
		// 默认播种的平台级角色（TenantId 未设置）：不应回填租户名。
		if item.GetTenantId() == 0 {
			require.Empty(t, item.GetTenantName(),
				"平台级角色不应回填租户名")
			sawPlatformRoleWithoutTenant = true
		}
	}
	require.True(t, sawTenantRole, "列表应包含新建的租户级角色")
	require.True(t, sawPlatformRoleWithoutTenant, "列表应包含默认播种的平台级角色")
}

// TestRoleServiceSqlite_GetEnrichment 验证 Get 单条查询的 TenantName 回填。
func TestRoleServiceSqlite_GetEnrichment(t *testing.T) {
	svc := newRoleServiceForTest(t)
	ctx := storetest.NewSystemContext(context.Background())

	tenant, err := svc.tenantRepo.Create(ctx, &operatorV1.Tenant{
		Name:        trans.Ptr("RoleSvc 富集租户乙"),
		Code:        trans.Ptr("ROLESVC_TENANT_B"),
		Status:      operatorV1.Tenant_ON.Enum(),
		Type:        operatorV1.Tenant_TRIAL.Enum(),
		AuditStatus: operatorV1.Tenant_APPROVED.Enum(),
	})
	require.NoError(t, err)
	require.NotNil(t, tenant.Id)

	err = svc.roleRepo.Create(ctx, &operatorV1.CreateRoleRequest{
		Data: &operatorV1.Role{
			TenantId: tenant.Id,
			Name:     trans.Ptr("RoleSvc 租户角色乙"),
			Code:     trans.Ptr("ROLESVC_ROLE_TENANT_B"),
			Status:   operatorV1.Role_ON.Enum(),
			Type:     operatorV1.Role_TENANT.Enum(),
		},
	})
	require.NoError(t, err)

	// RoleRepo.Create 只返回 error，角色 ID 从列表反查（按唯一 code 定位）。
	listResp, err := svc.List(ctx, &paginationV1.PagingRequest{})
	require.NoError(t, err)
	var roleID uint32
	var found bool
	for _, item := range listResp.GetItems() {
		if item.GetCode() == "ROLESVC_ROLE_TENANT_B" {
			roleID = item.GetId()
			found = true
		}
	}
	require.True(t, found, "创建后列表应包含新角色")

	resp, err := svc.Get(ctx, &operatorV1.GetRoleRequest{
		QueryBy: &operatorV1.GetRoleRequest_Id{Id: roleID},
	})
	require.NoError(t, err)
	require.Equal(t, "RoleSvc 租户角色乙", resp.GetName())
	require.Equal(t, "RoleSvc 富集租户乙", resp.GetTenantName(),
		"租户级角色的 Get 应回填租户名")
}

// TestRoleServiceSqlite_CreateAndDelete 验证 Create 的操作人注入与
// Delete 的非保护角色删除路径。
func TestRoleServiceSqlite_CreateAndDelete(t *testing.T) {
	svc := newRoleServiceForTest(t)
	baseCtx := storetest.NewSystemContext(context.Background())
	opCtx := auth.NewContext(baseCtx, &operatorV1.UserTokenPayload{UserId: 4242})

	// Create：操作人注入 CreatedBy，角色本体与角色元数据同事务落库。
	_, err := svc.Create(opCtx, &operatorV1.CreateRoleRequest{
		Data: &operatorV1.Role{
			Name:   trans.Ptr("RoleSvc 创建角色甲"),
			Code:   trans.Ptr("ROLESVC_CREATE_A"),
			Status: operatorV1.Role_ON.Enum(),
			Type:   operatorV1.Role_TENANT.Enum(),
		},
	})
	require.NoError(t, err, "service.Create 应走完操作人注入与事务创建")

	var createdID uint32
	listResp, err := svc.List(baseCtx, &paginationV1.PagingRequest{})
	require.NoError(t, err)
	var found bool
	for _, item := range listResp.GetItems() {
		if item.GetCode() == "ROLESVC_CREATE_A" {
			found = true
			createdID = item.GetId()
			require.EqualValues(t, 4242, item.GetCreatedBy(),
				"CreatedBy 应为操作人注入的 ID")
		}
	}
	require.True(t, found, "创建后列表应包含新角色")

	// Delete：非保护角色应删除成功（含关联行清理）。
	_, err = svc.Delete(baseCtx, &operatorV1.DeleteRoleRequest{
		QueryBy: &operatorV1.DeleteRoleRequest_Id{Id: createdID},
	})
	require.NoError(t, err, "非保护角色的 service.Delete 应成功")

	after, err := svc.List(baseCtx, &paginationV1.PagingRequest{})
	require.NoError(t, err)
	for _, item := range after.GetItems() {
		require.NotEqual(t, "ROLESVC_CREATE_A", item.GetCode(),
			"删除后列表不应再包含该角色")
	}
}
