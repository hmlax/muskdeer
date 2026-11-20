package audit

import "strings"

// 数据分类码，与前端 i18n（enum.dataAccessAuditLog.dataCategory.*）一一对应。
const (
	CategoryUserData      = "USER_DATA"
	CategoryOrgData       = "ORG_DATA"
	CategoryAccessControl = "ACCESS_CONTROL"
	CategoryTenantData    = "TENANT_DATA"
	CategoryMessage       = "MESSAGE_DATA"
	CategoryAuditLog      = "AUDIT_LOG"
	CategorySystemConfig  = "SYSTEM_CONFIG"
	CategoryUnknown       = "UNKNOWN"
)

// ClassifyTable 按表名映射数据分类码；未匹配返回 UNKNOWN。
func ClassifyTable(table string) string {
	t := strings.ToLower(strings.Trim(table, `"`))
	switch {
	case strings.HasSuffix(t, "_audit_logs"), t == "sys_policy_evaluation_logs":
		return CategoryAuditLog
	case t == "sys_users", strings.HasPrefix(t, "sys_user_"):
		return CategoryUserData
	case strings.HasPrefix(t, "sys_org_units"),
		strings.HasPrefix(t, "sys_positions"),
		strings.HasPrefix(t, "sys_membership"):
		return CategoryOrgData
	case t == "sys_tenants", t == "sys_plans", strings.HasPrefix(t, "sys_plan_"):
		return CategoryTenantData
	case strings.HasPrefix(t, "sys_internal_message"):
		return CategoryMessage
	case t == "sys_apis", t == "sys_menus",
		strings.HasPrefix(t, "sys_roles"),
		strings.HasPrefix(t, "sys_role_"),
		strings.HasPrefix(t, "sys_permission"),
		strings.HasPrefix(t, "sys_login_polic"):
		return CategoryAccessControl
	case strings.HasPrefix(t, "sys_dict"),
		t == "sys_languages",
		t == "sys_files",
		strings.HasPrefix(t, "sys_tasks"):
		return CategorySystemConfig
	default:
		return CategoryUnknown
	}
}
