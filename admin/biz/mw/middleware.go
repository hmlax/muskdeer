package mw

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/tx7do/go-crud/viewer"
	authzEngine "github.com/tx7do/kratos-authz/engine"
	"github.com/tx7do/kratos-bootstrap/bootstrap"

	"github.com/hmlax/muskdeer/admin/internal/transport"
	"github.com/hmlax/muskdeer/deer/management/httpmeta"
	"github.com/hmlax/muskdeer/deer/management/middleware/auth"
	applogging "github.com/hmlax/muskdeer/deer/management/middleware/logging"
	"github.com/hmlax/muskdeer/deer/management/portalscope"
	appViewer "github.com/hmlax/muskdeer/deer/management/viewer"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"go.opentelemetry.io/otel/trace"
)

var publicOperations = map[string]bool{
	operatorV1.OperationMediaServiceReadImage:                    true,
	operatorV1.OperationAuthenticationServiceLogin:               true,
	operatorV1.OperationAuthenticationServiceGenerateCaptcha:     true,
	operatorV1.OperationAuthenticationServiceVerifyCaptcha:       true,
	operatorV1.OperationAuthenticationServiceRefreshToken:        true,
	operatorV1.OperationMfaServiceVerifyMFAChallenge:             true,
	operatorV1.OperationAccessKeyServiceIssueToken:               true,
	operatorV1.OperationAuthenticationServiceForgotPassword:      true,
	operatorV1.OperationAuthenticationServiceResetPasswordByCode: true,
	operatorV1.OperationAuthenticationServiceGetTenantBranding:   true,
	operatorV1.OperationAuthenticationServiceGetSsoLoginInfo:     true,
	operatorV1.OperationAuthenticationServiceGetSsoLoginUrl:      true,
	operatorV1.OperationAuthenticationServiceSsoLogin:            true,
}

// NewRestMiddleware builds native Hertz middleware; every non-public route fails closed.
func NewRestMiddleware(boot *bootstrap.Context, checker auth.AccessTokenChecker, tenant auth.TenantAccessChecker, gateway operatorV1.GatewayServiceClient) ([]app.HandlerFunc, error) {
	writeAudit := func(ctx context.Context, record *operatorV1.AuditRecord) error {
		_, err := gateway.WriteAudit(ctx, record)
		if err != nil {
			slog.ErrorContext(ctx, "write HTTP audit", "error", err)
		}
		return err
	}
	recorder, err := applogging.NewRecorder(
		applogging.WithWriteApiLogFunc(func(ctx context.Context, data *operatorV1.ApiAuditLog) error {
			return writeAudit(ctx, &operatorV1.AuditRecord{Record: &operatorV1.AuditRecord_Api{Api: data}})
		}),
		applogging.WithWriteLoginLogFunc(func(ctx context.Context, data *operatorV1.LoginAuditLog) error {
			return writeAudit(ctx, &operatorV1.AuditRecord{Record: &operatorV1.AuditRecord_Login{Login: data}})
		}),
		applogging.WithWriteOperationAuditLogFunc(func(ctx context.Context, data *operatorV1.OperationAuditLog) error {
			return writeAudit(ctx, &operatorV1.AuditRecord{Record: &operatorV1.AuditRecord_Operation{Operation: data}})
		}),
		applogging.WithWritePermissionAuditLogFunc(func(ctx context.Context, data *operatorV1.PermissionAuditLog) error {
			return writeAudit(ctx, &operatorV1.AuditRecord{Record: &operatorV1.AuditRecord_Permission{Permission: data}})
		}),
		applogging.WithWriteDataAccessAuditLogFunc(func(ctx context.Context, data *operatorV1.DataAccessAuditLog) error {
			return writeAudit(ctx, &operatorV1.AuditRecord{Record: &operatorV1.AuditRecord_DataAccess{DataAccess: data}})
		}),
	)
	if err != nil {
		return nil, err
	}
	engine := newEvalLoggingEngine(&policyEngine{}, &policyRepo{client: gateway})
	return []app.HandlerFunc{
		func(ctx context.Context, c *app.RequestContext) {
			exchange, ok := httpmeta.FromContext(ctx)
			if !ok {
				transport.FailRequest(c, auth.ErrWrongContext)
				return
			}
			started := time.Now()
			ctx = recorder.Begin(ctx, exchange)
			c.Next(ctx)
			// Audits must survive a cancelled browser request, but remain time bounded.
			auditContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer cancel()
			recorder.Finish(auditContext, exchange, transport.RequestError(c), time.Since(started))
			boot.GetLogger().Info(ctx, "HTTP request", "method", exchange.RequestValue.Method, "route", exchange.Route, "status", c.Response.StatusCode(), "latency_ms", time.Since(started).Milliseconds())
		},
		transport.RecoverRequest(),
		func(ctx context.Context, c *app.RequestContext) {
			exchange, ok := httpmeta.FromContext(ctx)
			if !ok {
				transport.FailRequest(c, auth.ErrWrongContext)
				return
			}
			ctx, err := ResolvePortal(ctx, gateway)
			if err != nil {
				transport.FailRequest(c, err)
				return
			}
			if publicOperations[exchange.OperationName] {
				c.Next(ctx)
				return
			}
			authorization := exchange.RequestValue.Header.Get("Authorization")
			if !strings.HasPrefix(authorization, "Bearer ") || len(authorization) == len("Bearer ") {
				transport.FailRequest(c, auth.ErrMissingBearerToken)
				return
			}
			if checker == nil {
				transport.FailRequest(c, auth.ErrAccessTokenCheckerNotConfigured)
				return
			}
			valid, actor := checker.IsValidAccessToken(ctx, strings.TrimPrefix(authorization, "Bearer "), false)
			if !valid || actor == nil {
				transport.FailRequest(c, auth.ErrAccessTokenExpired)
				return
			}
			ctx = auth.NewContext(ctx, actor)
			if err := portalscope.CheckTenant(ctx, actor.GetTenantId()); err != nil {
				transport.FailRequest(c, err)
				return
			}
			user := viewer.NewUserContext(
				uint64(actor.GetUserId()), uint64(actor.GetTenantId()), uint64(actor.GetOrgUnitId()),
				trace.SpanContextFromContext(ctx).TraceID().String(),
				appViewer.BuildDataScopes(actor.GetDataScopes(), actor.GetDataScopeUnitIds(), actor.GetDataScope()),
			)
			ctx = viewer.WithContext(ctx, user)
			if tenant != nil && actor.GetTenantId() > 0 {
				if err := tenant.CheckTenantAccess(ctx, actor.GetTenantId(), exchange.Route, exchange.RequestValue.Method); err != nil {
					transport.FailRequest(c, err)
					return
				}
			}
			ctx, err = checkBoundary(ctx, exchange, gateway)
			if err != nil {
				transport.FailRequest(c, err)
				return
			}
			allowed := false
			for _, role := range actor.GetRoles() {
				allowed, err = engine.IsAuthorized(ctx, authzEngine.Subject(role), authzEngine.Action(exchange.RequestValue.Method), authzEngine.Resource(exchange.Route), "")
				if err != nil {
					transport.FailRequest(c, err)
					return
				}
				if allowed {
					break
				}
			}
			if !allowed {
				transport.FailRequest(c, operatorV1.ErrorAdminForbidden(errorUnauthorizedAccess))
				return
			}
			c.Next(ctx)
		},
	}, nil
}
