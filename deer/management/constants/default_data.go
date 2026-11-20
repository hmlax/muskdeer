package constants

import (
	"strconv"
	"time"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"

	"github.com/tx7do/go-utils/timeutil"
	"github.com/tx7do/go-utils/trans"
)

const (
	// DefaultAdminUserName 系统初始化默认管理员用户名
	DefaultAdminUserName = "admin"

	// DefaultUserPassword 系统初始化默认密码（管理员与普通用户统一，须满足
	// 口令复杂度策略（go-utils/password）：≥8位且至少3类字符——种子凭证经 prepareCredential
	// 入库时会做复杂度校验，不达标会被拒、初始化半途而废，admin 从此无法登录）
	DefaultUserPassword = "Abcd@1234"

	// PlatformTenantID 平台管理员租户ID
	PlatformTenantID = uint32(0)
)

// DefaultPermissionGroups 系统初始化默认权限组数据
var DefaultPermissionGroups = []*operatorV1.PermissionGroup{
	{
		// Id:        trans.Ptr(uint32(1)),
		Name:      trans.Ptr("系统管理"),
		Path:      trans.Ptr("/"),
		Module:    trans.Ptr(SystemPermissionModule),
		SortOrder: trans.Ptr(uint32(1)),
		Status:    trans.Ptr(operatorV1.PermissionGroup_ON),
	},
	{
		// Id:        trans.Ptr(uint32(2)),
		ParentId:  trans.Ptr(uint32(1)),
		Name:      trans.Ptr("系统权限"),
		Path:      trans.Ptr("/1/2/"),
		Module:    trans.Ptr(SystemPermissionModule),
		SortOrder: trans.Ptr(uint32(1)),
		Status:    trans.Ptr(operatorV1.PermissionGroup_ON),
	},
	{
		// Id:        trans.Ptr(uint32(3)),
		ParentId:  trans.Ptr(uint32(1)),
		Name:      trans.Ptr("租户管理"),
		Path:      trans.Ptr("/1/3/"),
		Module:    trans.Ptr(SystemPermissionModule),
		SortOrder: trans.Ptr(uint32(2)),
		Status:    trans.Ptr(operatorV1.PermissionGroup_ON),
	},
	{
		// Id:        trans.Ptr(uint32(4)),
		ParentId:  trans.Ptr(uint32(1)),
		Name:      trans.Ptr("审计管理"),
		Path:      trans.Ptr("/1/4/"),
		Module:    trans.Ptr(SystemPermissionModule),
		SortOrder: trans.Ptr(uint32(3)),
		Status:    trans.Ptr(operatorV1.PermissionGroup_ON),
	},
	{
		// Id:        trans.Ptr(uint32(5)),
		ParentId:  trans.Ptr(uint32(1)),
		Name:      trans.Ptr("安全策略"),
		Path:      trans.Ptr("/1/5/"),
		Module:    trans.Ptr(SystemPermissionModule),
		SortOrder: trans.Ptr(uint32(4)),
		Status:    trans.Ptr(operatorV1.PermissionGroup_ON),
	},
}

// DefaultPermissions 系统初始化默认权限数据
var DefaultPermissions = []*operatorV1.Permission{
	{
		// Id:          trans.Ptr(uint32(1)),
		GroupId:     trans.Ptr(uint32(2)),
		Name:        trans.Ptr("访问后台"),
		Description: trans.Ptr("允许用户访问系统后台管理界面"),
		Code:        trans.Ptr(SystemAccessBackendPermissionCode),
		Status:      trans.Ptr(operatorV1.Permission_ON),
	},
	{
		// Id:          trans.Ptr(uint32(2)),
		GroupId:     trans.Ptr(uint32(2)),
		Name:        trans.Ptr("平台管理员权限"),
		Description: trans.Ptr("拥有系统所有功能的操作权限，可管理租户、用户、角色及所有资源"),
		Code:        trans.Ptr(SystemPlatformAdminPermissionCode),
		Status:      trans.Ptr(operatorV1.Permission_ON),
		// 后台左侧菜单读的是「角色 → 权限 → sys_permission_menus」这条链（GetNavigation 无超管绕过），
		// 所以 DefaultMenus 里加一行还不够，新菜单必须同时出现在这里，否则播种出来的平台管理员看不见它。
		// 已部署实例的权限行早已存在、这段不会再跑，须在「权限管理」里勾上新菜单。
		MenuIds: []uint32{
			1, 2, 3, 4,
			10, 11,
			20, 21, 22, 23, 24,
			30, 31, 32, 33, 34,

			50, 51, 52, 53, 54, 55, 56,
			60, 61, 62, 64, 65, 66, 69, 70,
		},
		// API bindings are reconciled by operation identity after catalog initialization.
	},
	{
		// Id:          trans.Ptr(uint32(3)),
		GroupId:     trans.Ptr(uint32(3)),
		Name:        trans.Ptr("租户管理员权限"),
		Description: trans.Ptr("拥有租户内所有功能的操作权限，可管理用户、角色及租户内所有资源"),
		Code:        trans.Ptr(SystemTenantManagerPermissionCode),
		Status:      trans.Ptr(operatorV1.Permission_ON),
		MenuIds: []uint32{
			1, 2,
			20, 21, 22, 23, 24,
			30, 32, 33, 34,

			50, 51,
			60, 61, 62,
		},
		// API bindings are reconciled by operation identity after catalog initialization.
	},

	{
		// Id:          trans.Ptr(uint32(4)),
		GroupId:     trans.Ptr(uint32(3)),
		Name:        trans.Ptr("管理租户"),
		Description: trans.Ptr("允许创建/修改/删除租户"),
		Code:        trans.Ptr(SystemManageTenantsPermissionCode),
		Status:      trans.Ptr(operatorV1.Permission_ON),
	},
	{
		// Id:          trans.Ptr(uint32(5)),
		GroupId:     trans.Ptr(uint32(4)),
		Name:        trans.Ptr("查看审计日志"),
		Description: trans.Ptr("允许查看系统操作日志"),
		Code:        trans.Ptr(SystemAuditLogsPermissionCode),
		Status:      trans.Ptr(operatorV1.Permission_ON),
	},
}

// DefaultRoles 系统初始化默认角色数据
var DefaultRoles = []*operatorV1.Role{
	{
		// Id:          trans.Ptr(uint32(1)),
		Name:        trans.Ptr(DefaultPlatformAdminRoleName),
		Code:        trans.Ptr(PlatformAdminRoleCode),
		Status:      trans.Ptr(operatorV1.Role_ON),
		Description: trans.Ptr("拥有系统所有功能的操作权限，可管理租户、用户、角色及所有资源"),
		IsProtected: trans.Ptr(true),
		Type:        trans.Ptr(operatorV1.Role_SYSTEM),
		SortOrder:   trans.Ptr(uint32(1)),
		Permissions: []uint32{1, 2, 4},
	},
	{
		// Id:          trans.Ptr(uint32(2)),
		Name:        trans.Ptr(DefaultTenantManagerRoleName + "模板"),
		Code:        trans.Ptr(TenantAdminTemplateRoleCode),
		Status:      trans.Ptr(operatorV1.Role_ON),
		Description: trans.Ptr("租户管理员角色，拥有租户内所有功能的操作权限，可管理用户、角色及租户内所有资源"),
		IsProtected: trans.Ptr(true),
		Type:        trans.Ptr(operatorV1.Role_TEMPLATE),
		SortOrder:   trans.Ptr(uint32(2)),
		Permissions: []uint32{1, 3},
	},
}

// DefaultRoleMetadata 系统初始化默认角色元数据
var DefaultRoleMetadata = []*operatorV1.RoleMetadata{
	{
		// Id:              trans.Ptr(uint32(1)),
		RoleId:          trans.Ptr(uint32(1)),
		IsTemplate:      trans.Ptr(false),
		TemplateVersion: trans.Ptr(int32(1)),
		Scope:           operatorV1.RoleMetadata_PLATFORM.Enum(),
		SyncPolicy:      operatorV1.RoleMetadata_AUTO.Enum(),
	},
	{
		// Id:              trans.Ptr(uint32(2)),
		RoleId:          trans.Ptr(uint32(2)),
		IsTemplate:      trans.Ptr(true),
		TemplateFor:     trans.Ptr(TenantAdminRoleCode),
		TemplateVersion: trans.Ptr(int32(1)),
		Scope:           operatorV1.RoleMetadata_TENANT.Enum(),
		SyncPolicy:      operatorV1.RoleMetadata_AUTO.Enum(),
	},
}

// DefaultUsers 系统初始化默认用户数据
var DefaultUsers = []*operatorV1.User{
	{
		// Id:       trans.Ptr(uint32(1)),
		TenantId: trans.Ptr(uint32(0)),
		Username: trans.Ptr(DefaultAdminUserName),
		Realname: trans.Ptr("喵个咪"),
		Nickname: trans.Ptr("鹳狸猿"),
		Region:   trans.Ptr("中国"),
		Email:    trans.Ptr("admin@gmail.com"),
	},
}

// DefaultUserCredentials 系统初始化默认用户凭证数据
var DefaultUserCredentials = []*operatorV1.UserCredential{
	{
		UserId:         trans.Ptr(uint32(1)),
		TenantId:       trans.Ptr(uint32(0)),
		IdentityType:   operatorV1.UserCredential_USERNAME.Enum(),
		Identifier:     trans.Ptr(DefaultAdminUserName),
		CredentialType: operatorV1.UserCredential_PASSWORD_HASH.Enum(),
		Credential:     trans.Ptr(DefaultUserPassword),
		IsPrimary:      trans.Ptr(true),
		Status:         operatorV1.UserCredential_ENABLED.Enum(),
	},
}

// DefaultUserRoles 系统初始化默认用户角色关系数据
var DefaultUserRoles = []*operatorV1.UserRole{
	{
		UserId:    trans.Ptr(uint32(1)),
		TenantId:  trans.Ptr(uint32(0)),
		RoleId:    trans.Ptr(uint32(1)),
		IsPrimary: trans.Ptr(true),
		Status:    operatorV1.UserRole_ACTIVE.Enum(),
	},
}

// DefaultMemberships 系统初始化默认用户成员关系数据
var DefaultMemberships = []*operatorV1.Membership{
	{
		UserId:    trans.Ptr(uint32(1)),
		TenantId:  trans.Ptr(uint32(0)),
		Status:    operatorV1.Membership_ACTIVE.Enum(),
		IsPrimary: trans.Ptr(true),
		RoleIds:   []uint32{1},
	},
}

// DefaultLanguages 系统初始化默认语言数据
var DefaultLanguages = []*operatorV1.Language{
	{
		LanguageCode: trans.Ptr("zh-CN"),
		LanguageName: trans.Ptr("中文（简体）"),
		NativeName:   trans.Ptr("简体中文"),
		IsDefault:    trans.Ptr(true),
		IsEnabled:    trans.Ptr(true),
		SortOrder:    trans.Uint32(0),
	},
	{
		LanguageCode: trans.Ptr("zh-TW"),
		LanguageName: trans.Ptr("中文（繁体）"),
		NativeName:   trans.Ptr("繁體中文"),
		IsDefault:    trans.Ptr(false),
		IsEnabled:    trans.Ptr(true),
		SortOrder:    trans.Uint32(100),
	},
	{
		LanguageCode: trans.Ptr("en-US"),
		LanguageName: trans.Ptr("英语"),
		NativeName:   trans.Ptr("English"),
		IsDefault:    trans.Ptr(false),
		IsEnabled:    trans.Ptr(true),
		SortOrder:    trans.Uint32(1),
	},
	{
		LanguageCode: trans.Ptr("ja-JP"),
		LanguageName: trans.Ptr("日语"),
		NativeName:   trans.Ptr("日本語"),
		IsDefault:    trans.Ptr(false),
		IsEnabled:    trans.Ptr(true),
		SortOrder:    trans.Uint32(100),
	},
	{
		LanguageCode: trans.Ptr("ko-KR"),
		LanguageName: trans.Ptr("韩语"),
		NativeName:   trans.Ptr("한국어"),
		IsDefault:    trans.Ptr(false),
		IsEnabled:    trans.Ptr(true),
		SortOrder:    trans.Uint32(100),
	},
	{
		LanguageCode: trans.Ptr("es-ES"),
		LanguageName: trans.Ptr("西班牙语"),
		NativeName:   trans.Ptr("Español"),
		IsDefault:    trans.Ptr(false),
		IsEnabled:    trans.Ptr(true),
		SortOrder:    trans.Uint32(100),
	},
	{
		LanguageCode: trans.Ptr("fr-FR"),
		LanguageName: trans.Ptr("法语"),
		NativeName:   trans.Ptr("Français"),
		IsDefault:    trans.Ptr(false),
		IsEnabled:    trans.Ptr(true),
		SortOrder:    trans.Uint32(100),
	},
}

// ComponentToModule 根据前端组件路径前缀推断所属业务功能模块。
// 用于菜单的业务分类；权限由角色与 API 授权控制。
func ComponentToModule(component string) operatorV1.Module_Module {
	switch {
	case len(component) >= 13 && component[:13] == "app/muskdeer/":
		return operatorV1.Module_MUSKDEER
	case component == "BasicLayout" || component == "":
		return operatorV1.Module_MODULE_UNSPECIFIED
	case len(component) >= 10 && component[:10] == "dashboard/":
		return operatorV1.Module_DASHBOARD
	case len(component) >= 8 && component[:8] == "app/opm/":
		return operatorV1.Module_OPM
	case len(component) >= 11 && component[:11] == "app/system/":
		return operatorV1.Module_SYSTEM
	case len(component) >= 9 && component[:9] == "app/dict/":
		return operatorV1.Module_DICT
	case len(component) >= 11 && component[:11] == "app/tenant/":
		return operatorV1.Module_TENANT
	case len(component) >= 15 && component[:15] == "app/permission/":
		return operatorV1.Module_PERMISSION
	case len(component) >= 8 && component[:8] == "app/log/":
		return operatorV1.Module_LOG
	case len(component) >= 9 && component[:9] == "app/file/":
		return operatorV1.Module_FILE
	default:
		return operatorV1.Module_MODULE_UNSPECIFIED
	}
}

// DefaultMenus 系统初始化默认菜单数据
var DefaultMenus = []*operatorV1.Menu{
	{
		Id:        trans.Ptr(uint32(1)),
		ParentId:  nil,
		Type:      operatorV1.Menu_CATALOG.Enum(),
		Name:      trans.Ptr("Dashboard"),
		Path:      trans.Ptr("/dashboard"),
		Component: trans.Ptr("BasicLayout"),
		Status:    operatorV1.Menu_ON.Enum(),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Order:     trans.Ptr(int32(1)),
			Title:     trans.Ptr("page.dashboard.title"),
			Icon:      trans.Ptr("lucide:layout-dashboard"),
			Authority: []string{"sys:platform_admin", "sys:tenant_manager"},
		},
	},
	{
		Id:        trans.Ptr(uint32(2)),
		ParentId:  trans.Ptr(uint32(1)),
		Type:      operatorV1.Menu_MENU.Enum(),
		Name:      trans.Ptr("Analytics"),
		Path:      trans.Ptr("/analytics"),
		Component: trans.Ptr("dashboard/analytics/index.vue"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Order:     trans.Ptr(int32(1)),
			Title:     trans.Ptr("page.dashboard.analytics"),
			Icon:      trans.Ptr("lucide:area-chart"),
			Authority: []string{"sys:platform_admin", "sys:tenant_manager"},
			AffixTab:  trans.Ptr(true),
		},
	},

	{
		Id:        trans.Ptr(uint32(3)),
		ParentId:  nil,
		Type:      operatorV1.Menu_CATALOG.Enum(),
		Name:      trans.Ptr("Profile"),
		Path:      trans.Ptr("/profile"),
		Component: trans.Ptr("BasicLayout"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Title:      trans.Ptr("menu.profile.settings"),
			HideInMenu: trans.Ptr(true),
		},
	},
	{
		Id:        trans.Ptr(uint32(4)),
		ParentId:  trans.Ptr(uint32(3)),
		Type:      operatorV1.Menu_MENU.Enum(),
		Name:      trans.Ptr("ProfilePage"),
		Path:      trans.Ptr("/profile"),
		Component: trans.Ptr("app/opm/user/profile/index.vue"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Title:      trans.Ptr("menu.profile.settings"),
			Icon:       trans.Ptr("lucide:user-pen"),
			HideInMenu: trans.Ptr(true),
		},
	},

	{
		Id:        trans.Ptr(uint32(10)),
		ParentId:  nil,
		Type:      operatorV1.Menu_CATALOG.Enum(),
		Name:      trans.Ptr("TenantManagement"),
		Path:      trans.Ptr("/tenant"),
		Redirect:  trans.Ptr("/tenant/members"),
		Component: trans.Ptr("BasicLayout"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Order:     trans.Ptr(int32(2000)),
			Title:     trans.Ptr("menu.tenant.moduleName"),
			Icon:      trans.Ptr("lucide:building-2"),
			Authority: []string{"sys:platform_admin"},
		},
	},
	{
		Id:        trans.Ptr(uint32(11)),
		ParentId:  trans.Ptr(uint32(10)),
		Type:      operatorV1.Menu_MENU.Enum(),
		Name:      trans.Ptr("TenantMemberManagement"),
		Path:      trans.Ptr("members"),
		Component: trans.Ptr("app/tenant/tenant/index.vue"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Order:     trans.Ptr(int32(1)),
			Title:     trans.Ptr("menu.tenant.member"),
			Icon:      trans.Ptr("lucide:users"),
			Authority: []string{"sys:platform_admin"},
			AffixTab:  trans.Ptr(true),
		},
	},

	{
		Id:        trans.Ptr(uint32(20)),
		ParentId:  nil,
		Type:      operatorV1.Menu_CATALOG.Enum(),
		Name:      trans.Ptr("OrganizationalPersonnelManagement"),
		Path:      trans.Ptr("/opm"),
		Redirect:  trans.Ptr("/opm/users"),
		Component: trans.Ptr("BasicLayout"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Order:     trans.Ptr(int32(2001)),
			Title:     trans.Ptr("menu.opm.moduleName"),
			Icon:      trans.Ptr("lucide:users"),
			KeepAlive: trans.Ptr(true),
			Authority: []string{"sys:platform_admin", "sys:tenant_manager"},
		},
	},
	{
		Id:        trans.Ptr(uint32(21)),
		ParentId:  trans.Ptr(uint32(20)),
		Type:      operatorV1.Menu_MENU.Enum(),
		Name:      trans.Ptr("OrgUnitManagement"),
		Path:      trans.Ptr("org-units"),
		Component: trans.Ptr("app/opm/org_unit/index.vue"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Order:     trans.Ptr(int32(1)),
			Title:     trans.Ptr("menu.opm.orgUnit"),
			Icon:      trans.Ptr("lucide:layers"),
			Authority: []string{"sys:platform_admin", "sys:tenant_manager"},
		},
	},
	{
		Id:        trans.Ptr(uint32(22)),
		ParentId:  trans.Ptr(uint32(20)),
		Type:      operatorV1.Menu_MENU.Enum(),
		Name:      trans.Ptr("PositionManagement"),
		Path:      trans.Ptr("positions"),
		Component: trans.Ptr("app/opm/position/index.vue"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Order:     trans.Ptr(int32(2)),
			Title:     trans.Ptr("menu.opm.position"),
			Icon:      trans.Ptr("lucide:briefcase"),
			Authority: []string{"sys:platform_admin", "sys:tenant_manager"},
		},
	},
	{
		Id:        trans.Ptr(uint32(23)),
		ParentId:  trans.Ptr(uint32(20)),
		Type:      operatorV1.Menu_MENU.Enum(),
		Name:      trans.Ptr("UserManagement"),
		Path:      trans.Ptr("users"),
		Component: trans.Ptr("app/opm/user/list/index.vue"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Order:     trans.Ptr(int32(3)),
			Title:     trans.Ptr("menu.opm.user"),
			Icon:      trans.Ptr("lucide:user"),
			Authority: []string{"sys:platform_admin", "sys:tenant_manager"},
		},
	},
	{
		Id:        trans.Ptr(uint32(24)),
		ParentId:  trans.Ptr(uint32(20)),
		Type:      operatorV1.Menu_MENU.Enum(),
		Name:      trans.Ptr("UserDetail"),
		Path:      trans.Ptr("users/detail/:id"),
		Component: trans.Ptr("app/opm/user/detail/index.vue"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Title:      trans.Ptr("menu.opm.userDetail"),
			Authority:  []string{"sys:platform_admin", "sys:tenant_manager"},
			HideInMenu: trans.Ptr(true),
		},
	},

	{
		Id:        trans.Ptr(uint32(30)),
		ParentId:  nil,
		Type:      operatorV1.Menu_CATALOG.Enum(),
		Name:      trans.Ptr("PermissionManagement"),
		Path:      trans.Ptr("/permission"),
		Redirect:  trans.Ptr("/permission/codes"),
		Component: trans.Ptr("BasicLayout"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Order:     trans.Ptr(int32(2002)),
			Title:     trans.Ptr("menu.permission.moduleName"),
			Icon:      trans.Ptr("lucide:shield-check"),
			KeepAlive: trans.Ptr(true),
			Authority: []string{"sys:platform_admin", "sys:tenant_manager"},
		},
	},
	{
		Id:        trans.Ptr(uint32(31)),
		ParentId:  trans.Ptr(uint32(30)),
		Type:      operatorV1.Menu_MENU.Enum(),
		Name:      trans.Ptr("PermissionPointManagement"),
		Path:      trans.Ptr("codes"),
		Component: trans.Ptr("app/permission/permission/index.vue"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Title:     trans.Ptr("menu.permission.permission"),
			Icon:      trans.Ptr("lucide:shield-ellipsis"),
			Order:     trans.Ptr(int32(1)),
			Authority: []string{"sys:platform_admin"},
		},
	},
	{
		Id:        trans.Ptr(uint32(32)),
		ParentId:  trans.Ptr(uint32(30)),
		Type:      operatorV1.Menu_MENU.Enum(),
		Name:      trans.Ptr("RoleManagement"),
		Path:      trans.Ptr("roles"),
		Component: trans.Ptr("app/permission/role/index.vue"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Title:     trans.Ptr("menu.permission.role"),
			Icon:      trans.Ptr("lucide:shield-user"),
			Order:     trans.Ptr(int32(4)),
			Authority: []string{"sys:platform_admin", "sys:tenant_manager"},
		},
	},
	{
		Id:        trans.Ptr(uint32(33)),
		ParentId:  trans.Ptr(uint32(30)),
		Type:      operatorV1.Menu_MENU.Enum(),
		Name:      trans.Ptr("MenuManagement"),
		Path:      trans.Ptr("menus"),
		Component: trans.Ptr("app/permission/menu/index.vue"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Title:     trans.Ptr("menu.permission.menu"),
			Icon:      trans.Ptr("lucide:square-menu"),
			Order:     trans.Ptr(int32(2)),
			Authority: []string{"sys:platform_admin"},
		},
	},
	{
		Id:        trans.Ptr(uint32(34)),
		ParentId:  trans.Ptr(uint32(30)),
		Type:      operatorV1.Menu_MENU.Enum(),
		Name:      trans.Ptr("APIManagement"),
		Path:      trans.Ptr("apis"),
		Component: trans.Ptr("app/permission/api/index.vue"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Title:     trans.Ptr("menu.permission.api"),
			Icon:      trans.Ptr("lucide:route"),
			Order:     trans.Ptr(int32(3)),
			Authority: []string{"sys:platform_admin"},
		},
	},

	{
		Id:        trans.Ptr(uint32(50)),
		ParentId:  nil,
		Type:      operatorV1.Menu_CATALOG.Enum(),
		Name:      trans.Ptr("LogAuditManagement"),
		Path:      trans.Ptr("/log"),
		Redirect:  trans.Ptr("/log/login-audit-logs"),
		Component: trans.Ptr("BasicLayout"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Order:     trans.Ptr(int32(2004)),
			Title:     trans.Ptr("menu.log.moduleName"),
			Icon:      trans.Ptr("lucide:logs"),
			KeepAlive: trans.Ptr(true),
			Authority: []string{"sys:platform_admin"},
		},
	},
	{
		Id:        trans.Ptr(uint32(51)),
		ParentId:  trans.Ptr(uint32(50)),
		Type:      operatorV1.Menu_MENU.Enum(),
		Name:      trans.Ptr("LoginAuditLog"),
		Path:      trans.Ptr("login-audit-logs"),
		Component: trans.Ptr("app/log/login_audit_log/index.vue"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Title:     trans.Ptr("menu.log.loginAuditLog"),
			Icon:      trans.Ptr("lucide:user-lock"),
			Order:     trans.Ptr(int32(1)),
			Authority: []string{"sys:platform_admin"},
		},
	},
	{
		Id:        trans.Ptr(uint32(52)),
		ParentId:  trans.Ptr(uint32(50)),
		Type:      operatorV1.Menu_MENU.Enum(),
		Name:      trans.Ptr("ApiAuditLog"),
		Path:      trans.Ptr("api-audit-logs"),
		Component: trans.Ptr("app/log/api_audit_log/index.vue"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Title:     trans.Ptr("menu.log.apiAuditLog"),
			Icon:      trans.Ptr("lucide:file-clock"),
			Order:     trans.Ptr(int32(2)),
			Authority: []string{"sys:platform_admin"},
		},
	},
	{
		Id:        trans.Ptr(uint32(53)),
		ParentId:  trans.Ptr(uint32(50)),
		Type:      operatorV1.Menu_MENU.Enum(),
		Name:      trans.Ptr("OperationAuditLog"),
		Path:      trans.Ptr("operation-audit-logs"),
		Component: trans.Ptr("app/log/operation_audit_log/index.vue"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Title:     trans.Ptr("menu.log.operationAuditLog"),
			Icon:      trans.Ptr("lucide:shield-ellipsis"),
			Order:     trans.Ptr(int32(3)),
			Authority: []string{"sys:platform_admin"},
		},
	},
	{
		Id:        trans.Ptr(uint32(54)),
		ParentId:  trans.Ptr(uint32(50)),
		Type:      operatorV1.Menu_MENU.Enum(),
		Name:      trans.Ptr("DataAccessAuditLog"),
		Path:      trans.Ptr("data-access-audit-logs"),
		Component: trans.Ptr("app/log/data_access_audit_log/index.vue"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Title:     trans.Ptr("menu.log.dataAccessAuditLog"),
			Icon:      trans.Ptr("lucide:shield-check"),
			Order:     trans.Ptr(int32(4)),
			Authority: []string{"sys:platform_admin"},
		},
	},
	{
		Id:        trans.Ptr(uint32(55)),
		ParentId:  trans.Ptr(uint32(50)),
		Type:      operatorV1.Menu_MENU.Enum(),
		Name:      trans.Ptr("PermissionAuditLog"),
		Path:      trans.Ptr("permission-audit-logs"),
		Component: trans.Ptr("app/log/permission_audit_log/index.vue"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Title:     trans.Ptr("menu.log.permissionAuditLog"),
			Icon:      trans.Ptr("lucide:shield-alert"),
			Order:     trans.Ptr(int32(5)),
			Authority: []string{"sys:platform_admin"},
		},
	},
	{
		Id:        trans.Ptr(uint32(56)),
		ParentId:  trans.Ptr(uint32(50)),
		Type:      operatorV1.Menu_MENU.Enum(),
		Name:      trans.Ptr("PolicyEvaluationLog"),
		Path:      trans.Ptr("policy-evaluation-logs"),
		Component: trans.Ptr("app/log/policy_evaluation_log/index.vue"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Title:     trans.Ptr("menu.log.policyEvaluationLog"),
			Icon:      trans.Ptr("lucide:gavel"),
			Order:     trans.Ptr(int32(6)),
			Authority: []string{"sys:platform_admin"},
		},
	},

	{
		Id:        trans.Ptr(uint32(60)),
		ParentId:  nil,
		Type:      operatorV1.Menu_CATALOG.Enum(),
		Name:      trans.Ptr("System"),
		Path:      trans.Ptr("/system"),
		Redirect:  trans.Ptr("/system/dict"),
		Component: trans.Ptr("BasicLayout"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Order:     trans.Ptr(int32(2005)),
			Title:     trans.Ptr("menu.system.moduleName"),
			Icon:      trans.Ptr("lucide:settings"),
			KeepAlive: trans.Ptr(true),
			Authority: []string{"sys:platform_admin", "sys:tenant_manager"},
		},
	},
	{
		Id:        trans.Ptr(uint32(61)),
		ParentId:  trans.Ptr(uint32(60)),
		Type:      operatorV1.Menu_MENU.Enum(),
		Name:      trans.Ptr("DictManagement"),
		Path:      trans.Ptr("dict"),
		Component: trans.Ptr("app/system/dict/index.vue"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Title:     trans.Ptr("menu.system.dict"),
			Icon:      trans.Ptr("lucide:library-big"),
			Order:     trans.Ptr(int32(1)),
			Authority: []string{"sys:platform_admin"},
		},
	},
	{
		Id:        trans.Ptr(uint32(62)),
		ParentId:  trans.Ptr(uint32(60)),
		Type:      operatorV1.Menu_MENU.Enum(),
		Name:      trans.Ptr("FileManagement"),
		Path:      trans.Ptr("files"),
		Component: trans.Ptr("app/system/file/index.vue"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Title:     trans.Ptr("menu.system.file"),
			Icon:      trans.Ptr("lucide:file-search"),
			Order:     trans.Ptr(int32(2)),
			Authority: []string{"sys:platform_admin", "sys:tenant_manager"},
		},
	},

	{
		Id:        trans.Ptr(uint32(64)),
		ParentId:  trans.Ptr(uint32(60)),
		Type:      operatorV1.Menu_MENU.Enum(),
		Name:      trans.Ptr("LoginPolicyManagement"),
		Path:      trans.Ptr("login-policies"),
		Component: trans.Ptr("app/system/login_policy/index.vue"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Title:     trans.Ptr("menu.system.loginPolicy"),
			Icon:      trans.Ptr("lucide:shield-x"),
			Order:     trans.Ptr(int32(4)),
			Authority: []string{"sys:platform_admin"},
		},
	},
	{
		Id:        trans.Ptr(uint32(65)),
		ParentId:  trans.Ptr(uint32(60)),
		Type:      operatorV1.Menu_MENU.Enum(),
		Name:      trans.Ptr("LanguageManagement"),
		Path:      trans.Ptr("languages"),
		Component: trans.Ptr("app/system/language/index.vue"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Title:     trans.Ptr("menu.system.language"),
			Icon:      trans.Ptr("lucide:globe"),
			Order:     trans.Ptr(int32(5)),
			Authority: []string{"sys:platform_admin"},
		},
	},
	{
		Id:        trans.Ptr(uint32(70)),
		ParentId:  trans.Ptr(uint32(60)),
		Type:      operatorV1.Menu_MENU.Enum(),
		Name:      trans.Ptr("AccessKeyManagement"),
		Path:      trans.Ptr("access-keys"),
		Component: trans.Ptr("app/system/access_key/index.vue"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Title:     trans.Ptr("menu.system.accessKeys"),
			Icon:      trans.Ptr("lucide:key-round"),
			Order:     trans.Ptr(int32(11)),
			Authority: []string{"sys:platform_admin"},
		},
	},
	{
		Id:        trans.Ptr(uint32(66)),
		ParentId:  trans.Ptr(uint32(60)),
		Type:      operatorV1.Menu_MENU.Enum(),
		Name:      trans.Ptr("ConfigManagement"),
		Path:      trans.Ptr("configs"),
		Component: trans.Ptr("app/system/config/index.vue"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Title:     trans.Ptr("menu.system.config"),
			Icon:      trans.Ptr("lucide:sliders-horizontal"),
			Order:     trans.Ptr(int32(10)),
			Authority: []string{"sys:platform_admin"},
		},
	},

	{
		Id:        trans.Ptr(uint32(69)),
		ParentId:  trans.Ptr(uint32(60)),
		Type:      operatorV1.Menu_MENU.Enum(),
		Name:      trans.Ptr("OnlineSessionManagement"),
		Path:      trans.Ptr("online-sessions"),
		Component: trans.Ptr("app/system/online_session/index.vue"),
		CreatedAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
		Meta: &operatorV1.MenuMeta{
			Title:     trans.Ptr("menu.system.onlineSessions"),
			Icon:      trans.Ptr("lucide:monitor"),
			Order:     trans.Ptr(int32(6)),
			Authority: []string{"sys:platform_admin"},
		},
	},
}

// DefaultConfigs 系统初始化内置平台参数（等保口令策略阈值）。
// 键与默认值单�源定义于本包，保证种子行与 accessor 缺省回退一致；
// 服务启动时按键缺一补一（键已存在、值已被管理员改过的行不覆盖），is_built_in 可改不可删。
var DefaultConfigs = []*operatorV1.Config{
	{
		Key:       trans.Ptr(ConfigKeyPasswordMinLen),
		Name:      trans.Ptr("Password minimum length"),
		Value:     trans.Ptr(strconv.Itoa(DefaultPasswordMinLen)),
		ValueType: operatorV1.Config_INT.Enum(),
		IsBuiltIn: trans.Ptr(true),
	},
	{
		Key:       trans.Ptr(ConfigKeyPasswordMaxAgeDays),
		Name:      trans.Ptr("Password maximum age in days (0 disables expiry)"),
		Value:     trans.Ptr(strconv.Itoa(DefaultPasswordMaxAgeDays)),
		ValueType: operatorV1.Config_INT.Enum(),
		IsBuiltIn: trans.Ptr(true),
	},
	{
		Key:       trans.Ptr(ConfigKeyPasswordHistoryCount),
		Name:      trans.Ptr("Password history retention count (0 disables history check)"),
		Value:     trans.Ptr(strconv.Itoa(DefaultPasswordHistoryCount)),
		ValueType: operatorV1.Config_INT.Enum(),
		IsBuiltIn: trans.Ptr(true),
	},
}
