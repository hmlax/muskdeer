// Package rpcbridge carries trusted gateway context over typed gRPC calls.
package rpcbridge

import (
	"context"
	"encoding/base64"
	"net/http"
	"strconv"

	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/transport"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/tx7do/go-crud/viewer"
	"google.golang.org/protobuf/proto"

	"github.com/hmlax/muskdeer/deer/management/httpmeta"
	"github.com/hmlax/muskdeer/deer/management/middleware/auth"
	"github.com/hmlax/muskdeer/deer/management/portalscope"
	appViewer "github.com/hmlax/muskdeer/deer/management/viewer"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

const (
	actorKey            = "x-gateway-actor"
	gatewayUserAgentKey = "x-gateway-user-agent"
)

var forwardedHeaders = []string{"Cookie", "User-Agent", "X-Captcha-Id", "X-Captcha-Value", "X-Forwarded-For", "X-Forwarded-Proto", "X-Real-IP", "X-Request-ID", "Traceparent"}

func forwardedHeaderKey(key string) string {
	if key == "User-Agent" {
		// gRPC reserves user-agent and replaces it with its own client identifier.
		return gatewayUserAgentKey
	}
	return key
}

// Client forwards only allowlisted HTTP metadata and the principal validated by admin.
func Client() middleware.Middleware {
	return func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			outbound, ok := transport.FromClientContext(ctx)
			if !ok {
				return nil, auth.ErrWrongContext
			}
			if actor, err := auth.FromContext(ctx); err == nil && actor != nil {
				data, err := proto.Marshal(actor)
				if err != nil {
					return nil, err
				}
				outbound.RequestHeader().Set(actorKey, base64.RawStdEncoding.EncodeToString(data))
			}
			if scope, ok := portalscope.FromContext(ctx); ok {
				outbound.RequestHeader().Set(portalscope.TenantIDMetadata, strconv.FormatUint(uint64(scope.TenantID), 10))
				outbound.RequestHeader().Set(portalscope.TenantCodeMetadata, scope.Code)
			}
			inbound, hasInbound := transport.FromServerContext(ctx)
			if exchange, ok := httpmeta.FromContext(ctx); ok {
				for _, key := range forwardedHeaders {
					if value := exchange.RequestValue.Header.Get(key); value != "" {
						outbound.RequestHeader().Set(forwardedHeaderKey(key), value)
					}
				}
				request := exchange.RequestValue
				outbound.RequestHeader().Set("x-gateway-path", request.URL.Path)
				outbound.RequestHeader().Set("x-gateway-method", request.Method)
				outbound.RequestHeader().Set("x-gateway-remote", request.RemoteAddr)
				if request.TLS != nil {
					outbound.RequestHeader().Set("X-Forwarded-Proto", "https")
				}
				return next(ctx, req)
			}
			if hasInbound {
				for _, key := range forwardedHeaders {
					if v := inbound.RequestHeader().Get(key); v != "" {
						outbound.RequestHeader().Set(forwardedHeaderKey(key), v)
					}
				}
				if httpInbound, ok := inbound.(khttp.Transporter); ok && httpInbound.Request() != nil {
					request := httpInbound.Request()
					outbound.RequestHeader().Set("x-gateway-path", request.URL.Path)
					outbound.RequestHeader().Set("x-gateway-method", request.Method)
					outbound.RequestHeader().Set("x-gateway-remote", request.RemoteAddr)
					if request.TLS != nil {
						outbound.RequestHeader().Set("X-Forwarded-Proto", "https")
					}
				}
			}
			return next(ctx, req)
		}
	}
}

type httpMetadata struct {
	transport.Transporter
	request *http.Request
}

func (t *httpMetadata) Request() *http.Request { return t.request }
func (t *httpMetadata) PathTemplate() string   { return t.request.URL.Path }

// Server restores context only; JWT/RBAC/tenant entry checks run in admin.
// operator is an internal service and must only be reachable from trusted services.
func Server() middleware.Middleware {
	return func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			tr, ok := transport.FromServerContext(ctx)
			if !ok {
				return nil, auth.ErrWrongContext
			}
			if raw := tr.RequestHeader().Get(portalscope.TenantIDMetadata); raw != "" {
				id, err := strconv.ParseUint(raw, 10, 32)
				if err != nil {
					return nil, auth.ErrInvalidRequest
				}
				code, err := portalscope.ParseCode([]string{tr.RequestHeader().Get(portalscope.TenantCodeMetadata)})
				if err != nil || (id == 0) != (code == "") {
					return nil, auth.ErrInvalidRequest
				}
				ctx = portalscope.NewContext(ctx, portalscope.Scope{
					TenantID: uint32(id),
					Code:     code,
				})
			}
			if value := tr.RequestHeader().Get(actorKey); value != "" {
				data, err := base64.RawStdEncoding.DecodeString(value)
				if err != nil {
					return nil, auth.ErrInvalidRequest
				}
				actor := new(operatorV1.UserTokenPayload)
				if err := proto.Unmarshal(data, actor); err != nil {
					return nil, auth.ErrInvalidRequest
				}
				ctx = auth.NewContext(ctx, actor)
				ctx = viewer.WithContext(ctx, viewer.NewUserContext(uint64(actor.GetUserId()), uint64(actor.GetTenantId()), uint64(actor.GetOrgUnitId()), "", appViewer.BuildDataScopes(actor.GetDataScopes(), actor.GetDataScopeUnitIds(), actor.GetDataScope())))
			}
			path := tr.RequestHeader().Get("x-gateway-path")
			if path == "" {
				path = "/"
			}
			method := tr.RequestHeader().Get("x-gateway-method")
			if method == "" {
				method = http.MethodPost
			}
			request, err := http.NewRequestWithContext(ctx, method, "http://admin"+path, nil)
			if err != nil {
				return nil, err
			}
			for _, key := range forwardedHeaders {
				if v := tr.RequestHeader().Get(forwardedHeaderKey(key)); v != "" {
					request.Header.Set(key, v)
				}
			}
			// Business context consumers read either Request() or RequestHeader().
			// Restore the HTTP identity in both, scoped to this RPC's metadata.
			tr.RequestHeader().Set("User-Agent", request.UserAgent())
			request.RemoteAddr = tr.RequestHeader().Get("x-gateway-remote")
			ctx = transport.NewServerContext(ctx, &httpMetadata{
				Transporter: tr,
				request:     request,
			})
			return next(ctx, req)
		}
	}
}
