package audit

import "testing"

func TestClassifyTable(t *testing.T) {
	cases := map[string]string{
		"sys_users":                       CategoryUserData,
		`"sys_user_credentials"`:          CategoryUserData,
		"sys_user_mfa_factors":            CategoryUserData,
		"sys_org_units":                   CategoryOrgData,
		"sys_positions":                   CategoryOrgData,
		"sys_membership_roles":            CategoryOrgData,
		"sys_roles":                       CategoryAccessControl,
		"sys_role_permissions":            CategoryAccessControl,
		"sys_permissions":                 CategoryAccessControl,
		"sys_permission_groups":           CategoryAccessControl,
		"sys_menus":                       CategoryAccessControl,
		"sys_apis":                        CategoryAccessControl,
		"sys_login_policies":              CategoryAccessControl,
		"sys_tenants":                     CategoryTenantData,
		"sys_plans":                       CategoryTenantData,
		"sys_plan_quotas":                 CategoryTenantData,
		"sys_internal_messages":           CategoryMessage,
		"sys_internal_message_recipients": CategoryMessage,
		"sys_data_access_audit_logs":      CategoryAuditLog,
		"sys_permission_audit_logs":       CategoryAuditLog,
		"sys_policy_evaluation_logs":      CategoryAuditLog,
		"sys_dict_entries":                CategorySystemConfig,
		"sys_languages":                   CategorySystemConfig,
		"sys_files":                       CategorySystemConfig,
		"sys_tasks":                       CategorySystemConfig,
		"order_info":                      CategoryUnknown,
	}
	for table, want := range cases {
		if got := ClassifyTable(table); got != want {
			t.Errorf("ClassifyTable(%q) = %v, want %v", table, got, want)
		}
	}
}
