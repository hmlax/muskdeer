package service

import "github.com/google/wire"

// ProviderSet provides the reusable administration services.
var ProviderSet = wire.NewSet(
	NewAccessKeyService,
	NewMuskdeerService,
	NewAdminPortalService,
	NewApiAuditLogService,
	NewApiService,
	NewAuditExportService,
	NewAuthenticationService,
	NewConfigService,
	NewDataAccessAuditLogService,
	NewDictEntryService,
	NewDictTypeService,
	NewFileService,
	NewFileTransferService,
	NewLanguageService,
	NewLoginAuditLogService,
	NewLoginPolicyService,
	NewMenuService,
	NewMfaService,
	NewOnlineSessionService,
	NewOperationAuditLogService,
	NewOrgUnitService,
	NewPermissionAuditLogService,
	NewPermissionGroupService,
	NewPermissionService,
	NewPolicyEvaluationLogService,
	NewPositionService,
	NewRoleService,
	NewTenantService,
	NewTransactionMailer,
	NewUserProfileService,
	NewUserService,
)
