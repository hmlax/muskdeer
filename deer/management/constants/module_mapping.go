package constants

import operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"

// ServiceTagToBusinessModule groups registered APIs for administration.
var ServiceTagToBusinessModule = map[string]operatorV1.Module_Module{
	"MuskdeerService":    operatorV1.Module_MUSKDEER,
	"ExportService":      operatorV1.Module_LOG,
	"AdminPortalService": operatorV1.Module_DASHBOARD,

	"UserService":        operatorV1.Module_OPM,
	"OrgUnitService":     operatorV1.Module_OPM,
	"PositionService":    operatorV1.Module_OPM,
	"UserProfileService": operatorV1.Module_OPM,
	"RoleService":        operatorV1.Module_OPM,
	"MfaService":         operatorV1.Module_OPM,
	"MediaService":       operatorV1.Module_FILE,

	"MenuService":            operatorV1.Module_PERMISSION,
	"ApiService":             operatorV1.Module_PERMISSION,
	"PermissionService":      operatorV1.Module_PERMISSION,
	"PermissionGroupService": operatorV1.Module_PERMISSION,

	"DictTypeService":     operatorV1.Module_DICT,
	"DictEntryService":    operatorV1.Module_DICT,
	"LanguageService":     operatorV1.Module_SYSTEM,
	"FileService":         operatorV1.Module_FILE,
	"FileTransferService": operatorV1.Module_FILE,
	"LoginPolicyService":  operatorV1.Module_SYSTEM,
	"ConfigService":       operatorV1.Module_SYSTEM,
	"AccessKeyService":    operatorV1.Module_SYSTEM,

	"TenantService": operatorV1.Module_TENANT,

	"ApiAuditLogService":         operatorV1.Module_LOG,
	"LoginAuditLogService":       operatorV1.Module_LOG,
	"OperationAuditLogService":   operatorV1.Module_LOG,
	"DataAccessAuditLogService":  operatorV1.Module_LOG,
	"PermissionAuditLogService":  operatorV1.Module_LOG,
	"PolicyEvaluationLogService": operatorV1.Module_LOG,
	"OnlineSessionService":       operatorV1.Module_SYSTEM,

	"AuthenticationService": operatorV1.Module_DASHBOARD,
}
