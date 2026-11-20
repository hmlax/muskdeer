package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	"github.com/tx7do/go-utils/sliceutil"
	"github.com/tx7do/go-utils/trans"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/hmlax/muskdeer/operator/internal/data"

	"github.com/hmlax/muskdeer/deer/management/constants"
	"github.com/hmlax/muskdeer/deer/management/middleware/auth"
	"github.com/hmlax/muskdeer/deer/management/muskdeer"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

type AdminPortalService struct {
	log *bLogger.Helper

	menuRepo       *data.MenuRepo
	roleRepo       *data.RoleRepo
	userRepo       data.UserRepo
	permissionRepo *data.PermissionRepo

	apiRepo *data.ApiRepo
}

func NewAdminPortalService(
	ctx *bootstrap.Context,
	menuRepo *data.MenuRepo,
	roleRepo *data.RoleRepo,
	userRepo data.UserRepo,
	permissionRepo *data.PermissionRepo,

	apiRepo *data.ApiRepo,
) *AdminPortalService {
	return &AdminPortalService{
		log:            ctx.NewLoggerHelper("admin-portal/service/admin-service"),
		menuRepo:       menuRepo,
		roleRepo:       roleRepo,
		userRepo:       userRepo,
		permissionRepo: permissionRepo,

		apiRepo: apiRepo,
	}
}

func (s *AdminPortalService) menuListToQueryString(menus []uint32, onlyButton bool) string {
	var ids []string
	for _, menu := range menus {
		ids = append(ids, fmt.Sprintf("\"%d\"", menu))
	}
	idsStr := fmt.Sprintf("[%s]", strings.Join(ids, ", "))
	query := make(map[string]string)
	if menus != nil {
		query["id__in"] = idsStr
	}

	if onlyButton {
		query["type"] = operatorV1.Menu_BUTTON.String()
	} else {
		query["type__not"] = operatorV1.Menu_BUTTON.String()
	}

	query["status"] = "ON"

	queryStr, err := json.Marshal(query)
	if err != nil {
		return ""
	}

	return string(queryStr)
}

// queryMultipleRolesMenusByRoleCodes 使用RoleCodes查询菜单，即多个角色的菜单
func (s *AdminPortalService) queryMultipleRolesMenusByRoleCodes(ctx context.Context, roleIDs []uint32) ([]uint32, error) {
	var menuIDs []uint32
	var err error
	menuIDs, err = s.roleRepo.GetRolesPermissionMenuIDs(ctx, roleIDs)
	if err != nil {
		return nil, operatorV1.ErrorAdminInternalServerError("query roles menuIDs failed")
	}

	s.log.Infof(ctx, "queryMultipleRolesMenusByRoleCodes menuIDs: %+v", menuIDs)

	menuIDs = sliceutil.Unique(menuIDs)

	return menuIDs, nil
}
func (s *AdminPortalService) GetMyPermissionCode(ctx context.Context, _ *emptypb.Empty) (*operatorV1.ListPermissionCodeResponse, error) {
	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	user, err := s.userRepo.Get(ctx, &operatorV1.GetUserRequest{
		QueryBy: &operatorV1.GetUserRequest_Id{
			Id: operator.UserId,
		},
	})
	if err != nil {
		s.log.Errorf(ctx, "query user failed[%s]", err.Error())
		return nil, operatorV1.ErrorAdminInternalServerError("query user failed")
	}

	permissionIDs, err := s.roleRepo.ListPermissionIDsByRoleIDs(ctx, user.GetRoleIds())
	if err != nil {
		return nil, err
	}

	var permissionCodes []string
	permissionCodes, err = s.permissionRepo.GetPermissionCodesByIDs(ctx, permissionIDs)
	if err != nil {
		return nil, err
	}

	apiPermissions, err := s.effectiveAPIPermissions(ctx, operator, user.GetRoleIds())
	if err != nil {
		return nil, err
	}
	return &operatorV1.ListPermissionCodeResponse{
		Codes:          permissionCodes,
		HiddenFields:   operator.GetHiddenFields(),
		ApiPermissions: apiPermissions,
	}, nil
}

const apiGrantWildcard = "*"

func (s *AdminPortalService) effectiveAPIPermissions(ctx context.Context, actor *operatorV1.UserTokenPayload, roleIDs []uint32) ([]*operatorV1.ApiGrant, error) {
	if actor.GetIsPlatformAdmin() {
		return []*operatorV1.ApiGrant{{
			Path:   apiGrantWildcard,
			Method: apiGrantWildcard,
		}}, nil
	}
	ids, err := s.roleRepo.GetRolesPermissionApiIDs(ctx, roleIDs)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	apis, err := s.apiRepo.GetApiByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	grants := make([]*operatorV1.ApiGrant, 0, len(apis))
	for _, api := range apis {
		if api.GetPath() == "" || api.GetMethod() == "" {
			continue
		}
		grants = append(grants, &operatorV1.ApiGrant{
			Path:   api.GetPath(),
			Method: api.GetMethod(),
		})
	}
	return grants, nil
}

func (s *AdminPortalService) GetInitialContext(ctx context.Context, in *emptypb.Empty) (*operatorV1.InitialContextResponse, error) {
	permissions, err := s.GetMyPermissionCode(ctx, in)
	if err != nil {
		return nil, err
	}
	navigation, err := s.GetNavigation(ctx, in)
	if err != nil {
		return nil, err
	}
	return &operatorV1.InitialContextResponse{
		Menus:          navigation.GetItems(),
		Permissions:    permissions.GetCodes(),
		HiddenFields:   permissions.GetHiddenFields(),
		ApiPermissions: permissions.GetApiPermissions(),
	}, nil
}

func (s *AdminPortalService) fillRouteItem(menus []*operatorV1.Menu, platform bool) []*operatorV1.MenuRouteItem {
	if len(menus) == 0 {
		return nil
	}

	var routers []*operatorV1.MenuRouteItem

	for _, v := range menus {
		if !muskdeer.MenuAllowed(v.GetName(), platform) {
			continue
		}
		authority := v.GetMeta().GetAuthority()
		if !platform && len(authority) == 1 && authority[0] == constants.SystemPlatformAdminPermissionCode {
			continue
		}
		if v.GetStatus() != operatorV1.Menu_ON {
			continue
		}
		if v.GetType() == operatorV1.Menu_BUTTON {
			continue
		}

		item := &operatorV1.MenuRouteItem{
			Path:      v.Path,
			Component: v.Component,
			Name:      v.Name,
			Redirect:  v.Redirect,
			Alias:     v.Alias,
			Meta:      v.Meta,
		}

		if len(v.Children) > 0 {
			item.Children = s.fillRouteItem(v.Children, platform)
		}

		if v.GetType() == operatorV1.Menu_CATALOG && len(item.Children) == 0 {
			continue
		}
		routers = append(routers, item)
	}

	return routers
}

func (s *AdminPortalService) GetNavigation(ctx context.Context, _ *emptypb.Empty) (*operatorV1.ListRouteResponse, error) {
	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	user, err := s.userRepo.Get(ctx, &operatorV1.GetUserRequest{
		QueryBy: &operatorV1.GetUserRequest_Id{
			Id: operator.UserId,
		},
	})
	if err != nil {
		s.log.Errorf(ctx, "query user failed[%s]", err.Error())
		return nil, operatorV1.ErrorAdminInternalServerError("query user failed")
	}

	// 多角色的菜单
	roleMenus, err := s.queryMultipleRolesMenusByRoleCodes(ctx, user.GetRoleIds())
	if err != nil {
		return nil, err
	}
	if len(roleMenus) == 0 {
		return &operatorV1.ListRouteResponse{}, nil
	}

	menuList, err := s.menuRepo.List(ctx, &paginationV1.PagingRequest{
		NoPaging: trans.Ptr(true),
		FilteringType: &paginationV1.PagingRequest_Query{
			Query: s.menuListToQueryString(nil, false),
		},
	}, true)
	if err != nil {
		s.log.Errorf(ctx, "list route failed [%s]", err.Error())
		return nil, operatorV1.ErrorAdminInternalServerError("list route failed")
	}
	granted := make(map[uint32]bool, len(roleMenus))
	for _, id := range roleMenus {
		granted[id] = true
	}
	menuList.Items = filterRoleMenuTree(menuList.Items, granted)

	return &operatorV1.ListRouteResponse{Items: s.fillRouteItem(menuList.Items, operator.GetTenantId() == 0)}, nil
}
