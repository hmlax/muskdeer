package bootstrap

import (
	"google.golang.org/grpc"

	"github.com/hmlax/muskdeer/admin/biz/handler"
	"github.com/hmlax/muskdeer/admin/biz/mw"
	"github.com/hmlax/muskdeer/admin/biz/router"
	"github.com/hmlax/muskdeer/admin/internal/transport"
)

// The integration suite exercises the production packages together, including
// generated routes, authentication, gRPC forwarding, and the real listeners.
type Router = transport.Router
type hzBinding = transport.Binding

var (
	newAPIRouter         = transport.NewAPIRouter
	bindRequest          = transport.BindRequest
	failRequest          = transport.FailRequest
	traceIDFromContext   = mw.TraceIDFromContext
	clientIPFromContext  = mw.ClientIPFromContext
	NewRestMiddleware    = mw.NewRestMiddleware
	newTenantChecker     = mw.NewTenantChecker
	newExportHTTPService = handler.NewExportHTTPService

	registerSwagger                        = router.RegisterSwagger
	RegisterFileTransferServiceHertzServer = router.RegisterFileTransferServiceHertzServer
	RegisterExportServiceHertzServer       = router.RegisterExportServiceHertzServer
)

func registerServices(h *transport.Router, conn grpc.ClientConnInterface) {
	router.GeneratedRegister(h, handler.NewServices(conn, nil))
}
