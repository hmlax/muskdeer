package data

import (
	"context"
	"fmt"
	"strings"

	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/api"
	"github.com/hmlax/muskdeer/operator/internal/data/store/permission"
	"github.com/hmlax/muskdeer/operator/internal/data/store/permissionapi"

	"github.com/hmlax/muskdeer/deer/management/constants"
	"github.com/hmlax/muskdeer/deer/management/muskdeer"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

// EnsureBackendAccessAPIs maintains the system backend-access capability using
// protobuf operations, since catalog IDs can differ between deployments/syncs.
// Business APIs keep their separately assigned permissions and tenant gates.
func (r *ApiRepo) EnsureBackendAccessAPIs(ctx context.Context) (err error) {
	operations := []string{
		operatorV1.OperationUserProfileServiceGetUser,
		operatorV1.OperationAdminPortalServiceGetMyPermissionCode,
		operatorV1.OperationAdminPortalServiceGetNavigation,
		operatorV1.OperationAdminPortalServiceGetInitialContext,
		operatorV1.OperationAuthenticationServiceLogout,
	}
	for i, operation := range operations {
		// OpenAPI operationId uses Service_Method; Kratos uses /package.Service/Method.
		operations[i] = strings.ReplaceAll(strings.TrimPrefix(operation, "/operator.v1."), "/", "_")
	}
	apiIDs, err := r.dbClient.Client().Api.Query().Where(api.OperationIn(operations...)).IDs(ctx)
	if err != nil {
		return fmt.Errorf("find backend access APIs: %w", err)
	}
	if len(apiIDs) == 0 {
		return nil
	}
	return r.replaceBuiltInAPIs(ctx, constants.SystemAccessBackendPermissionCode, apiIDs)
}

// EnsureBuiltInAdminAPIs resolves built-in capabilities from API operations,
// never from catalog row numbers. Tenant managers receive tenant-owned resources
// and personal settings; global configuration and platform administration stay excluded.
func (r *ApiRepo) EnsureBuiltInAdminAPIs(ctx context.Context) error {
	rows, err := r.dbClient.Client().Api.Query().All(ctx)
	if err != nil {
		return fmt.Errorf("read APIs for built-in capabilities: %w", err)
	}
	platformIDs := make([]uint32, 0, len(rows))
	tenantIDs := make([]uint32, 0, len(rows))
	for _, row := range rows {
		if strings.HasPrefix(row.GetOperation(), muskdeer.ServiceTag+"_") {
			method := strings.TrimPrefix(row.GetOperation(), muskdeer.ServiceTag+"_")
			if muskdeer.MethodAllowed(method, true) {
				platformIDs = append(platformIDs, row.ID)
			}
			if muskdeer.MethodAllowed(method, false) {
				tenantIDs = append(tenantIDs, row.ID)
			}
			continue
		}
		platformIDs = append(platformIDs, row.ID)
		if _, excluded := tenantManagerExcludedOperations[row.GetOperation()]; excluded {
			continue
		}
		service, _, _ := strings.Cut(row.GetOperation(), "_")
		_, tenantOwned := tenantManagerServices[service]
		_, tenantOperation := tenantManagerOperations[row.GetOperation()]
		if tenantOwned || tenantOperation {
			tenantIDs = append(tenantIDs, row.ID)
		}
	}
	if err := r.replaceBuiltInAPIs(ctx, constants.SystemPlatformAdminPermissionCode, platformIDs); err != nil {
		return err
	}
	return r.replaceBuiltInAPIs(ctx, constants.SystemTenantManagerPermissionCode, tenantIDs)
}

var tenantManagerServices = map[string]struct{}{

	"MuskdeerService": {},
	"UserService":     {}, "UserProfileService": {}, "OrgUnitService": {}, "PositionService": {}, "RoleService": {},
	"FileService": {}, "FileTransferService": {}, "MediaService": {},

	"OnlineSessionService": {}, "MfaService": {}, "AccessKeyService": {},
	"LoginAuditLogService": {}, "ApiAuditLogService": {}, "OperationAuditLogService": {},
	"DataAccessAuditLogService": {}, "PermissionAuditLogService": {}, "PolicyEvaluationLogService": {},
}

var tenantManagerOperations = map[string]struct{}{
	// List already limits the catalog to permissions held by the caller's tenant roles.
	"PermissionService_List": {},
}

var tenantManagerExcludedOperations = map[string]struct{}{
	"MuskdeerService_ListAccountBindings":      {},
	"MuskdeerService_CreateAccountBinding":     {},
	"MuskdeerService_SetAccountBindingEnabled": {},
	"OnlineSessionService_ListOnlineSession":   {},
	"OnlineSessionService_ForceLogoutSession":  {},
	"TaskService_StopAllTask":                  {},
	"TaskService_StartAllTask":                 {},
	"TaskService_RestartAllTask":               {},
	"AiContentService_RebuildSearchIndex":      {},
}

func (r *ApiRepo) replaceBuiltInAPIs(ctx context.Context, code string, apiIDs []uint32) (err error) {
	permissionID, err := r.dbClient.Client().Permission.Query().
		Where(permission.CodeEQ(code)).OnlyID(ctx)
	if store.IsNotFound(err) {
		// First startup constructs the catalog before the default permissions.
		return nil
	}
	if err != nil {
		return fmt.Errorf("find built-in permission %s: %w", code, err)
	}
	tx, err := r.dbClient.Client().Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin backend access reconciliation: %w", err)
	}
	defer store.MakeTxCleanup(tx, &err)()
	// Each capability owns its exact endpoint set. Replace links atomically to
	// repair stale numeric seeds without changing custom permissions.
	if _, err = tx.PermissionApi.Delete().Where(permissionapi.PermissionIDEQ(permissionID)).Exec(ctx); err != nil {
		return fmt.Errorf("clear backend access API links: %w", err)
	}
	for _, apiID := range apiIDs {
		if err = tx.PermissionApi.Create().SetPermissionID(permissionID).SetAPIID(apiID).Exec(ctx); err != nil {
			return fmt.Errorf("assign backend access API %d: %w", apiID, err)
		}
	}
	return nil
}
