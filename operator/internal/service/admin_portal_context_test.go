package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-utils/trans"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/hmlax/muskdeer/operator/internal/data"
	"github.com/hmlax/muskdeer/operator/internal/data/store/permission"
	storetest "github.com/hmlax/muskdeer/operator/internal/data/storetest"

	"github.com/hmlax/muskdeer/deer/management/constants"
	"github.com/hmlax/muskdeer/deer/management/middleware/auth"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

func TestAdminPortalInitialContextRejectsMissingActor(t *testing.T) {
	service := &AdminPortalService{}
	response, err := service.GetInitialContext(context.Background(), &emptypb.Empty{})
	require.Error(t, err)
	require.Nil(t, response)
}

func TestAdminPortalAPIPermissionsFollowEffectiveGrants(t *testing.T) {
	client := storetest.NewClientForTest(t)
	ctx := storetest.NewSystemContext(t.Context())
	role, err := client.Client().Role.Create().SetTenantID(42).SetName("custom reader").SetCode("custom-reader").Save(ctx)
	require.NoError(t, err)
	point, err := client.Client().Permission.Create().SetName("custom read permission").SetCode("qa:read").Save(ctx)
	require.NoError(t, err)
	endpoint, err := client.Client().Api.Create().SetPath("/admin/v1/users").SetMethod("GET").Save(ctx)
	require.NoError(t, err)
	require.NoError(t, client.Client().PermissionApi.Create().SetPermissionID(point.ID).SetAPIID(endpoint.ID).Exec(ctx))
	require.NoError(t, client.Client().RolePermission.Create().SetRoleID(role.ID).SetPermissionID(point.ID).SetTenantID(42).Exec(ctx))
	svc := &AdminPortalService{
		log:            bLogger.NewHelper(bLogger.NopLogger()),
		menuRepo:       data.NewMenuRepoForTest(client),
		roleRepo:       data.NewRoleRepoForTest(client),
		permissionRepo: data.NewPermissionRepoForTest(client),
		apiRepo:        data.NewApiRepoForTest(client),
		userRepo: &authSvcUserRepoStub{
			usersByID: map[uint32]*operatorV1.User{
				1: {
					Id:      trans.Ptr(uint32(1)),
					RoleIds: []uint32{role.ID},
				},
			},
		},
	}
	actor := auth.NewContext(ctx, &operatorV1.UserTokenPayload{
		UserId:   1,
		TenantId: trans.Ptr(uint32(42)),
	})
	response, err := svc.GetInitialContext(actor, &emptypb.Empty{})
	require.NoError(t, err)
	require.Len(t, response.ApiPermissions, 1)
	require.Equal(t, "GET", response.ApiPermissions[0].GetMethod())
	require.Equal(t, "/admin/v1/users", response.ApiPermissions[0].GetPath())
	_, err = client.Client().Permission.UpdateOneID(point.ID).SetStatus(permission.StatusOff).Save(ctx)
	require.NoError(t, err)
	response, err = svc.GetInitialContext(actor, &emptypb.Empty{})
	require.NoError(t, err)
	require.Empty(t, response.ApiPermissions)

	platform := auth.NewContext(ctx, &operatorV1.UserTokenPayload{
		UserId:          1,
		IsPlatformAdmin: trans.Ptr(true),
	})
	response, err = svc.GetInitialContext(platform, &emptypb.Empty{})
	require.NoError(t, err)
	require.Len(t, response.ApiPermissions, 1)
	require.Equal(t, apiGrantWildcard, response.ApiPermissions[0].GetMethod())
	require.Equal(t, apiGrantWildcard, response.ApiPermissions[0].GetPath())
}

func TestAdminPortalInitialContextCarriesFieldPermissions(t *testing.T) {
	client := storetest.NewClientForTest(t)
	service := &AdminPortalService{
		log:            bLogger.NewHelper(bLogger.NopLogger()),
		menuRepo:       data.NewMenuRepoForTest(client),
		roleRepo:       data.NewRoleRepoForTest(client),
		apiRepo:        data.NewApiRepoForTest(client),
		permissionRepo: data.NewPermissionRepoForTest(client),
		userRepo: &authSvcUserRepoStub{
			usersByID: map[uint32]*operatorV1.User{
				1: {Id: trans.Ptr(uint32(1))},
			},
		},
	}
	ctx := auth.NewContext(storetest.NewSystemContext(context.Background()), &operatorV1.UserTokenPayload{
		UserId:       1,
		HiddenFields: []string{"User.mobile"},
	})
	response, err := service.GetInitialContext(ctx, &emptypb.Empty{})
	require.NoError(t, err)
	require.Empty(t, response.GetMenus())
	require.Empty(t, response.GetPermissions())
	require.Equal(t, []string{"User.mobile"}, response.GetHiddenFields())
}

func TestNavigationKeepsCoreMenusAndPlatformBoundary(t *testing.T) {
	service := &AdminPortalService{}
	menus := []*operatorV1.Menu{
		{
			Type:   operatorV1.Menu_CATALOG.Enum(),
			Status: operatorV1.Menu_ON.Enum(),
			Children: []*operatorV1.Menu{{
				Name:   trans.Ptr("Tenants"),
				Type:   operatorV1.Menu_MENU.Enum(),
				Status: operatorV1.Menu_ON.Enum(),
				Meta: &operatorV1.MenuMeta{
					Authority: []string{constants.SystemPlatformAdminPermissionCode},
				},
			}},
		},
		{
			Name:   trans.Ptr("Users"),
			Type:   operatorV1.Menu_MENU.Enum(),
			Status: operatorV1.Menu_ON.Enum(),
		},
	}
	merchant := service.fillRouteItem(menus, false)
	require.Len(t, merchant, 1)
	require.Equal(t, "Users", merchant[0].GetName())
	require.Len(t, service.fillRouteItem(menus, true), 2)
	require.Len(t, menus[0].Children, 1, "filtering must preserve stored menu data")
}
