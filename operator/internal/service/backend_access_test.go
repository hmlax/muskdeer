package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-utils/trans"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/hmlax/muskdeer/deer/management/constants"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data/store/api"
	"github.com/hmlax/muskdeer/operator/internal/data/store/permission"
	"github.com/hmlax/muskdeer/operator/internal/data/store/permissionapi"
	storetest "github.com/hmlax/muskdeer/operator/internal/data/storetest"
)

func TestBackendAccessPermissionsFollowCatalogIDs(t *testing.T) {
	svc, client := newPermissionServiceForTest(t)
	ctx := storetest.NewSystemContext(context.Background())
	apiSvc := &ApiService{
		log:        svc.log,
		repo:       svc.apiRepo,
		authorizer: svc.authorizer,
	}
	for round := 0; round < 2; round++ {
		_, err := apiSvc.SyncApis(ctx, &emptypb.Empty{})
		require.NoError(t, err)
		svc.init()
		permissionID, err := client.Permission.Query().Where(permission.CodeEQ(constants.SystemAccessBackendPermissionCode)).OnlyID(ctx)
		require.NoError(t, err)
		links, err := client.PermissionApi.Query().Where(permissionapi.PermissionIDEQ(permissionID)).All(ctx)
		require.NoError(t, err)
		require.Len(t, links, 5, "bootstrap and catalog resync must grant the five base endpoints")
		ids := make([]uint32, 0, len(links))
		for _, link := range links {
			ids = append(ids, *link.APIID)
		}
		endpoints, err := client.Api.Query().Where(api.IDIn(ids...)).All(ctx)
		require.NoError(t, err)
		var routes []string
		for _, endpoint := range endpoints {
			routes = append(routes, *endpoint.Method+" "+*endpoint.Path)
		}
		require.ElementsMatch(t, []string{
			"GET /admin/v1/me",
			"GET /admin/v1/perm-codes",
			"GET /admin/v1/routes",
			"GET /admin/v1/initial-context",
			"POST /admin/v1/logout",
		}, routes, "base access must not grant unrelated business endpoints")
	}
}

func TestTenantManagerCapabilitiesUseOperations(t *testing.T) {
	svc, client := newPermissionServiceForTest(t)
	ctx := storetest.NewSystemContext(context.Background())
	apiSvc := &ApiService{
		log:        svc.log,
		repo:       svc.apiRepo,
		authorizer: svc.authorizer,
	}
	_, err := apiSvc.SyncApis(ctx, &emptypb.Empty{})
	require.NoError(t, err)
	svc.init()
	permissionID, err := client.Permission.Query().Where(permission.CodeEQ(constants.SystemTenantManagerPermissionCode)).OnlyID(ctx)
	require.NoError(t, err)
	links, err := client.PermissionApi.Query().Where(permissionapi.PermissionIDEQ(permissionID)).All(ctx)
	require.NoError(t, err)
	ids := make([]uint32, 0, len(links))
	for _, link := range links {
		ids = append(ids, link.GetApiId())
	}
	rows, err := client.Api.Query().Where(api.IDIn(ids...)).All(ctx)
	require.NoError(t, err)
	operations := make([]string, 0, len(rows))
	for _, row := range rows {
		operations = append(operations, row.GetOperation())
	}
	require.Contains(t, operations, "UserService_List")
	require.Contains(t, operations, "PermissionService_List")
	require.NotContains(t, operations, "PermissionService_Get")
	require.NotContains(t, operations, "PermissionService_Create")
	require.Contains(t, operations, "MfaService_GetMFAStatus")
	require.NotContains(t, operations, "ApiService_SyncApis")
	require.NotContains(t, operations, "OnlineSessionService_ListOnlineSession")
	require.Contains(t, operations, "OnlineSessionService_ListMyOnlineSession")
}

func TestAPICatalogSyncPreservesCustomPermissionIdentity(t *testing.T) {
	svc, client := newPermissionServiceForTest(t)
	ctx := storetest.NewSystemContext(context.Background())
	apiSvc := &ApiService{
		log:        svc.log,
		repo:       svc.apiRepo,
		authorizer: svc.authorizer,
	}
	_, err := apiSvc.SyncApis(ctx, &emptypb.Empty{})
	require.NoError(t, err)
	userList, err := client.Api.Query().Where(api.OperationEQ("UserService_List")).Only(ctx)
	require.NoError(t, err)
	custom, err := client.Permission.Create().SetCode("test:catalog:reader").SetName("Catalog reader").Save(ctx)
	require.NoError(t, err)
	require.NoError(t, client.PermissionApi.Create().SetPermissionID(custom.ID).SetAPIID(userList.ID).Exec(ctx))
	_, err = apiSvc.SyncApis(ctx, &emptypb.Empty{})
	require.NoError(t, err)
	after, err := client.Api.Query().Where(api.OperationEQ("UserService_List")).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, userList.ID, after.ID, "接口同步不能让自定义权限指向另一条接口")
	link, err := client.PermissionApi.Query().Where(permissionapi.PermissionIDEQ(custom.ID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, after.ID, link.GetApiId())
}

func TestAPICatalogSyncRollsBackDuplicateRoutes(t *testing.T) {
	svc, client := newPermissionServiceForTest(t)
	ctx := storetest.NewSystemContext(context.Background())
	item := &operatorV1.Api{
		Path:   trans.Ptr("/test/catalog"),
		Method: trans.Ptr("GET"),
	}
	err := svc.apiRepo.SyncCatalog(ctx, []*operatorV1.Api{item, item})
	require.ErrorContains(t, err, "duplicate API route")
	count, err := client.Api.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count, "同步失败必须回滚此前新增的接口")
}
