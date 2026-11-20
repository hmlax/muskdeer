package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-utils/trans"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"

	"github.com/hmlax/muskdeer/operator/internal/data"
	"github.com/hmlax/muskdeer/operator/internal/data/store"
	modelMenu "github.com/hmlax/muskdeer/operator/internal/data/store/menu"
	storetest "github.com/hmlax/muskdeer/operator/internal/data/storetest"

	"github.com/hmlax/muskdeer/deer/management/middleware/auth"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

// newMenuServiceForTest 白盒构造 MenuService，逐字段对齐 NewMenuService 的装配
// （log 用 NopLogger helper）；init()（默认菜单播种）不在构造时调用，由用例按需触发。
func newMenuServiceForTest(t *testing.T, dbClient *store.DBClient) *MenuService {
	t.Helper()
	return &MenuService{
		log:      bLogger.NewHelper(bLogger.NopLogger()),
		menuRepo: data.NewMenuRepoForTest(dbClient),
	}
}

// menuIDByName 直查 GORM 取指定名称菜单的主键。
func menuIDByName(t *testing.T, dbClient *store.DBClient, ctx context.Context, name string) uint32 {
	t.Helper()
	rows, err := dbClient.Client().Menu.Query().All(ctx)
	require.NoError(t, err)
	for _, r := range rows {
		if r.Name != nil && *r.Name == name {
			return r.ID
		}
	}
	t.Fatalf("未找到名称为 %s 的菜单", name)
	return 0
}

// TestMenuServiceSqlite_CreateAndGet_ParentChild 验证服务层创建父子菜单：
// ParentId 落库为指向父菜单的外键、枚举经转换器落库、操作人 ID 盖入 created_by；
// Get 按主键命中/未命中。
func TestMenuServiceSqlite_CreateAndGet_ParentChild(t *testing.T) {
	dbClient := storetest.NewClientForTest(t)
	svc := newMenuServiceForTest(t, dbClient)
	ctx := storetest.NewSystemContext(context.Background())
	opCtx := auth.NewContext(ctx, &operatorV1.UserTokenPayload{UserId: 7})

	_, err := svc.Create(opCtx, &operatorV1.CreateMenuRequest{
		Data: &operatorV1.Menu{
			Name:   trans.Ptr("服务层父菜单"),
			Path:   trans.Ptr("/svc-menu-parent"),
			Type:   operatorV1.Menu_MENU.Enum(),
			Status: operatorV1.Menu_ON.Enum(),
		},
	})
	require.NoError(t, err, "创建父菜单应成功")
	parentID := menuIDByName(t, dbClient, ctx, "服务层父菜单")

	_, err = svc.Create(opCtx, &operatorV1.CreateMenuRequest{
		Data: &operatorV1.Menu{
			Name:     trans.Ptr("服务层子菜单"),
			Path:     trans.Ptr("svc-child"),
			ParentId: &parentID,
			Type:     operatorV1.Menu_MENU.Enum(),
			Status:   operatorV1.Menu_ON.Enum(),
		},
	})
	require.NoError(t, err, "创建子菜单应成功")

	rows, err := dbClient.Client().Menu.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2, "menu 表应落库 2 条记录")
	for _, r := range rows {
		require.Equal(t, modelMenu.StatusOn, *r.Status, "status 应经转换器落库为 ON")
		require.Equal(t, modelMenu.TypeMenu, *r.Type, "type 应经转换器落库为 MENU")
		require.NotNil(t, r.CreatedBy, "created_by 应被服务层盖入操作人 ID")
		require.Equal(t, uint32(7), *r.CreatedBy, "created_by 应等于令牌声明中的操作人 ID")
		if r.Name != nil && *r.Name == "服务层子菜单" {
			require.NotNil(t, r.ParentID, "子菜单的 parent_id 应落库为指向父菜单的外键")
			require.Equal(t, parentID, *r.ParentID, "子菜单外键应指向父菜单")
		} else {
			require.Nil(t, r.ParentID, "父菜单不应有 parent_id")
		}
	}

	childID := menuIDByName(t, dbClient, ctx, "服务层子菜单")
	got, err := svc.Get(ctx, &operatorV1.GetMenuRequest{
		QueryBy: &operatorV1.GetMenuRequest_Id{Id: childID},
	})
	require.NoError(t, err, "按已存在主键查询应命中")
	require.Equal(t, "svc-child", got.GetPath(), "命中记录的 path 应与写入一致")

	_, err = svc.Get(ctx, &operatorV1.GetMenuRequest{
		QueryBy: &operatorV1.GetMenuRequest_Id{Id: 9999999},
	})
	require.Error(t, err, "按不存在主键查询应返回错误")
}

// TestMenuServiceSqlite_List_FlatWithContainsFilter 验证服务层 List 的扁平返回
// （treeTravel=false，不组装 Children）与 contains 模糊搜索语义（仓规：搜索条件一律 contains）。
func TestMenuServiceSqlite_List_FlatWithContainsFilter(t *testing.T) {
	dbClient := storetest.NewClientForTest(t)
	svc := newMenuServiceForTest(t, dbClient)
	ctx := storetest.NewSystemContext(context.Background())
	opCtx := auth.NewContext(ctx, &operatorV1.UserTokenPayload{UserId: 7})

	for _, name := range []string{"MARKERMENUALPHA 菜单", "无关菜单乙"} {
		_, err := svc.Create(opCtx, &operatorV1.CreateMenuRequest{
			Data: &operatorV1.Menu{
				Name:   trans.Ptr(name),
				Path:   trans.Ptr("/svc-menu-flat"),
				Type:   operatorV1.Menu_MENU.Enum(),
				Status: operatorV1.Menu_ON.Enum(),
			},
		})
		require.NoError(t, err)
	}

	all, err := svc.List(ctx, &paginationV1.PagingRequest{})
	require.NoError(t, err)
	require.Equal(t, uint64(2), all.Total, "无过滤时应统计全部 2 条")
	require.Len(t, all.Items, 2, "无过滤时应返回 2 条")
	for _, item := range all.Items {
		require.Empty(t, item.GetChildren(), "服务层 List 不组装树，Children 应为空")
	}

	filtered, err := svc.List(ctx, &paginationV1.PagingRequest{
		FilteringType: &paginationV1.PagingRequest_FilterExpr{
			FilterExpr: &paginationV1.FilterExpr{
				Type: paginationV1.ExprType_AND,
				Conditions: []*paginationV1.FilterCondition{
					{
						Field:      "name",
						Op:         paginationV1.Operator_CONTAINS,
						ValueOneof: &paginationV1.FilterCondition_Value{Value: "MARKERMENUALPHA"},
					},
				},
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, uint64(1), filtered.Total, "contains 过滤后 Total 应为 1")
	require.Len(t, filtered.Items, 1, "contains 过滤后只应返回 1 条")
	require.Contains(t, filtered.Items[0].GetName(), "MARKERMENUALPHA", "命中行应是携带标记的那条")
}

// TestMenuServiceSqlite_Update 验证服务层 Update 在单字段掩码下
// 只更新掩码内字段（name），掩码外字段（path）保持原值。
func TestMenuServiceSqlite_Update(t *testing.T) {
	dbClient := storetest.NewClientForTest(t)
	svc := newMenuServiceForTest(t, dbClient)
	ctx := storetest.NewSystemContext(context.Background())
	opCtx := auth.NewContext(ctx, &operatorV1.UserTokenPayload{UserId: 7})

	_, err := svc.Create(opCtx, &operatorV1.CreateMenuRequest{
		Data: &operatorV1.Menu{
			Name:   trans.Ptr("更新前菜单名"),
			Path:   trans.Ptr("/svc-menu-update"),
			Type:   operatorV1.Menu_MENU.Enum(),
			Status: operatorV1.Menu_ON.Enum(),
		},
	})
	require.NoError(t, err)
	createdID := menuIDByName(t, dbClient, ctx, "更新前菜单名")

	_, err = svc.Update(opCtx, &operatorV1.UpdateMenuRequest{
		Id:         createdID,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name"}},
		Data:       &operatorV1.Menu{Name: trans.Ptr("更新后菜单名")},
	})
	require.NoError(t, err, "单字段掩码更新应成功")

	after, err := dbClient.Client().Menu.Query().Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "更新后菜单名", *after.Name, "掩码内字段 name 应被更新")
	require.Equal(t, "/svc-menu-update", *after.Path, "掩码外字段 path 应保持原值")
}

// TestMenuServiceSqlite_Delete_RemovesSubtree 验证删除父菜单时同时删除后代，
// 与当前跨方言 DescendantIDs 实现保持一致，不残留无法访问的子菜单。
func TestMenuServiceSqlite_Delete_RemovesSubtree(t *testing.T) {
	dbClient := storetest.NewClientForTest(t)
	svc := newMenuServiceForTest(t, dbClient)
	ctx := storetest.NewSystemContext(context.Background())
	opCtx := auth.NewContext(ctx, &operatorV1.UserTokenPayload{UserId: 7})

	_, err := svc.Create(opCtx, &operatorV1.CreateMenuRequest{
		Data: &operatorV1.Menu{
			Name:   trans.Ptr("待删除父菜单"),
			Path:   trans.Ptr("/svc-menu-del-parent"),
			Type:   operatorV1.Menu_MENU.Enum(),
			Status: operatorV1.Menu_ON.Enum(),
		},
	})
	require.NoError(t, err)
	parentID := menuIDByName(t, dbClient, ctx, "待删除父菜单")

	_, err = svc.Create(opCtx, &operatorV1.CreateMenuRequest{
		Data: &operatorV1.Menu{
			Name:     trans.Ptr("待删除子菜单"),
			Path:     trans.Ptr("del-child"),
			ParentId: &parentID,
			Type:     operatorV1.Menu_MENU.Enum(),
			Status:   operatorV1.Menu_ON.Enum(),
		},
	})
	require.NoError(t, err)

	cnt, err := dbClient.Client().Menu.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, cnt, "删除前应有父子两条菜单")

	_, err = svc.Delete(opCtx, &operatorV1.DeleteMenuRequest{
		QueryBy: &operatorV1.DeleteMenuRequest_Id{Id: parentID},
	})
	require.NoError(t, err, "删除目标菜单本身应成功")

	remaining, err := dbClient.Client().Menu.Query().All(ctx)
	require.NoError(t, err)
	require.Empty(t, remaining, "父菜单与后代必须全部删除")
}

// TestMenuServiceSqlite_SyncMenus_MergeInsertsTree 验证服务层 SyncMenus 的
// MERGE 模式在空表上的行为：按传入树全量插入（缺失即新增），父子外键按树组装，
// 操作人 ID 被盖入每行的 created_by。
func TestMenuServiceSqlite_SyncMenus_MergeInsertsTree(t *testing.T) {
	dbClient := storetest.NewClientForTest(t)
	svc := newMenuServiceForTest(t, dbClient)
	ctx := storetest.NewSystemContext(context.Background())
	opCtx := auth.NewContext(ctx, &operatorV1.UserTokenPayload{UserId: 7})

	_, err := svc.SyncMenus(opCtx, &operatorV1.SyncMenusRequest{
		Mode: trans.Ptr(operatorV1.SyncMenusRequest_MERGE),
		Items: []*operatorV1.Menu{
			{
				Name:   trans.Ptr("同步根菜单"),
				Path:   trans.Ptr("/svc-sync-root"),
				Type:   operatorV1.Menu_MENU.Enum(),
				Status: operatorV1.Menu_ON.Enum(),
				Children: []*operatorV1.Menu{
					{
						Name:   trans.Ptr("同步子菜单"),
						Path:   trans.Ptr("sync-child"),
						Type:   operatorV1.Menu_MENU.Enum(),
						Status: operatorV1.Menu_ON.Enum(),
					},
				},
			},
		},
	})
	require.NoError(t, err, "MERGE 模式同步应成功")

	rows, err := dbClient.Client().Menu.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2, "同步后应插入根与子共 2 条菜单")
	var rootID, childParentID uint32
	var rootFound, childFound bool
	for _, r := range rows {
		if r.Name != nil && *r.Name == "同步根菜单" {
			rootFound = true
			rootID = r.ID
			require.Nil(t, r.ParentID, "同步树的根不应有 parent_id")
			require.Equal(t, modelMenu.TypeMenu, *r.Type, "type 应经转换器落库为 MENU")
			require.Equal(t, modelMenu.StatusOn, *r.Status, "status 应经转换器落库为 ON")
		}
		if r.Name != nil && *r.Name == "同步子菜单" {
			childFound = true
			require.NotNil(t, r.ParentID, "同步树的子节点应组装指向根的外键")
			childParentID = *r.ParentID
		}
		if r.CreatedBy != nil {
			require.Equal(t, uint32(7), *r.CreatedBy, "同步行 created_by 应盖入操作人 ID")
		} else {
			t.Fatal("同步行的 created_by 不应为空")
		}
	}
	require.True(t, rootFound, "应找到同步根菜单")
	require.True(t, childFound, "应找到同步子菜单")
	require.Equal(t, rootID, childParentID, "同步子菜单的外键应指向同步根菜单")
}
