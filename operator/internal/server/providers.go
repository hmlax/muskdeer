package server

import (
	"github.com/google/wire"
	"github.com/tx7do/go-utils/authorizer"

	"github.com/hmlax/muskdeer/operator/internal/data"
	"github.com/hmlax/muskdeer/operator/internal/service"
)

// ProviderSet supplies the internal gRPC transport and background worker.
var ProviderSet = wire.NewSet(NewGatewayService, NewGRPCServer)

// NewGatewayService connects service event publishers before transports start.
func NewGatewayService(
	policies authorizer.Provider,
	access *data.GatewayAccessRepo,
	apiAudit *data.ApiAuditLogRepo,
	loginAudit *data.LoginAuditLogRepo,
	operationAudit *data.OperationAuditLogRepo,
	permissionAudit *data.PermissionAuditLogRepo,
	dataAccessAudit *data.DataAccessAuditLogRepo,
	policyAudit *data.PolicyEvaluationLogRepo,
	auditExport *service.AuditExportService,

	files *service.FileTransferService,

) *GatewayService {
	gateway := &GatewayService{
		Policies:        policies,
		Access:          access,
		APIAudit:        apiAudit,
		LoginAudit:      loginAudit,
		OperationAudit:  operationAudit,
		PermissionAudit: permissionAudit,
		DataAccessAudit: dataAccessAudit,
		PolicyAudit:     policyAudit,
		AuditExport:     auditExport,

		Files: files,
	}

	return gateway
}
