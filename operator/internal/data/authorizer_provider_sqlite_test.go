package data

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	"github.com/hmlax/muskdeer/deer/management/constants"
	"github.com/hmlax/muskdeer/operator/internal/data/store/permission"
	"github.com/hmlax/muskdeer/operator/internal/data/store/role"
	storetest "github.com/hmlax/muskdeer/operator/internal/data/storetest"
)

func TestAuthorizerProviderPreservesSameRoleAcrossTenants(t *testing.T) {
	client := storetest.NewClientForTest(t)
	ctx := storetest.NewSystemContext(context.Background())
	provider := &AuthorizerProvider{
		log:      bLogger.NewHelper(bLogger.NopLogger()),
		roleRepo: NewRoleRepoForTest(client),
		apiRepo:  NewApiRepoForTest(client),
	}
	perm, err := client.Client().Permission.Create().SetCode("tenant-base").SetName("base access").Save(ctx)
	require.NoError(t, err)
	endpoint, err := client.Client().Api.Create().SetPath("/admin/v1/me").SetMethod("GET").Save(ctx)
	require.NoError(t, err)
	_, err = client.Client().PermissionApi.Create().SetPermissionID(perm.ID).SetAPIID(endpoint.ID).Save(ctx)
	require.NoError(t, err)
	page, err := client.Client().Menu.Create().SetName("authorized user page").Save(ctx)
	require.NoError(t, err)
	require.NoError(t, client.Client().PermissionMenu.Create().SetPermissionID(perm.ID).SetMenuID(page.ID).Exec(ctx))
	var roleIDs []uint32
	for _, tenantID := range []uint32{1, 2} {
		role, err := client.Client().Role.Create().SetTenantID(tenantID).
			SetCode(constants.TenantAdminRoleCode).SetName("tenant manager").Save(ctx)
		require.NoError(t, err)
		roleIDs = append(roleIDs, role.ID)
		_, err = client.Client().RolePermission.Create().SetTenantID(tenantID).
			SetRoleID(role.ID).SetPermissionID(perm.ID).Save(ctx)
		require.NoError(t, err)
	}

	policies, err := provider.ProvidePolicies(ctx)
	require.NoError(t, err)
	grants := policies[constants.TenantAdminRoleCode]
	require.Len(t, grants, 2, "same role code must retain grants in both tenant domains")
	require.ElementsMatch(t, []string{"1", "2"}, []string{grants[0].Domain, grants[1].Domain})

	_, err = client.Client().Role.UpdateOneID(roleIDs[0]).SetStatus(role.StatusOff).Save(ctx)
	require.NoError(t, err)
	policies, err = provider.ProvidePolicies(ctx)
	require.NoError(t, err)
	require.Len(t, policies[constants.TenantAdminRoleCode], 1)
	require.Equal(t, "2", policies[constants.TenantAdminRoleCode][0].Domain)
	disabledMenus, err := provider.roleRepo.GetRolesPermissionMenuIDs(ctx, roleIDs[:1])
	require.NoError(t, err)
	require.Empty(t, disabledMenus, "disabled roles cannot grant navigation through another active role")
	disabledPermissions, err := provider.roleRepo.ListPermissionIDsByRoleIDs(ctx, roleIDs[:1])
	require.NoError(t, err)
	require.Empty(t, disabledPermissions)

	_, err = client.Client().Permission.UpdateOneID(perm.ID).SetStatus(permission.StatusOff).Save(ctx)
	require.NoError(t, err)
	policies, err = provider.ProvidePolicies(ctx)
	require.NoError(t, err)
	require.Empty(t, policies, "disabled permission points cannot grant API access")
	menuIDs, err := provider.roleRepo.GetRolesPermissionMenuIDs(ctx, roleIDs)
	require.NoError(t, err)
	require.Empty(t, menuIDs, "disabled permission points cannot grant navigation")
	codes, err := provider.roleRepo.permissionRepo.GetPermissionCodesByIDs(ctx, []uint32{perm.ID})
	require.NoError(t, err)
	require.Empty(t, codes, "disabled permission points cannot appear in effective permission codes")

	_, err = client.Client().Permission.UpdateOneID(perm.ID).SetStatus(permission.StatusOn).Save(ctx)
	require.NoError(t, err)
	_, err = client.Client().Role.UpdateOneID(roleIDs[0]).SetStatus(role.StatusOn).Save(ctx)
	require.NoError(t, err)
	policies, err = provider.ProvidePolicies(ctx)
	require.NoError(t, err)
	require.Len(t, policies[constants.TenantAdminRoleCode], 2, "enabling restores the retained bindings")
}
