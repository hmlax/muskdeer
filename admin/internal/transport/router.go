package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	hertz "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/network/standard"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	conf "github.com/tx7do/kratos-bootstrap/api/gen/go/conf/v1"

	"github.com/hmlax/muskdeer/deer/management/httpmeta"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

const requestErrorKey = "admin.request.error"
const pathValuesKey = "admin.request.path_values"

type routeBinding struct {
	template  string
	operation string
	match     *regexp.Regexp
	fields    []string
	stream    bool
	handler   app.HandlerFunc
}

// Router registers protobuf routes directly on Hertz, including literal RPC suffixes.
type Router struct {
	*hertz.Hertz
	middleware []app.HandlerFunc
	timeout    time.Duration
	bindings   map[string][]routeBinding
}

func NewAPIRouter(cfg *conf.Bootstrap, middleware ...app.HandlerFunc) (*Router, error) {
	return NewRouter(cfg, nil, nil, middleware...)
}
func NewRouter(cfg *conf.Bootstrap, listener net.Listener, accept func(net.Conn) context.Context, middleware ...app.HandlerFunc) (*Router, error) {
	options := []config.Option{
		hertz.WithTransport(standard.NewTransporter),
		hertz.WithMaxRequestBodySize(70 << 20),
		hertz.WithSenseClientDisconnection(true),
		hertz.WithExitWaitTime(10 * time.Second),
		hertz.WithRedirectTrailingSlash(false),
		hertz.WithHandleMethodNotAllowed(true),
	}
	if listener != nil {
		options = append(options, hertz.WithListener(listener))
	}
	if accept != nil {
		options = append(options, hertz.WithOnAccept(accept))
	}
	router := &Router{
		Hertz:      hertz.New(options...),
		middleware: middleware,
		bindings:   make(map[string][]routeBinding),
	}
	if rest := cfg.GetServer().GetRest(); rest != nil {
		router.timeout = rest.GetTimeout().AsDuration()
		if cors := rest.GetCors(); cors != nil {
			router.Use(corsMiddleware(cors))
		}
	}
	router.Use(RecoverRequest())
	router.NoRoute(func(_ context.Context, c *app.RequestContext) {
		FailRequest(c, operatorV1.ErrorAdminNotFound(errorRouteNotFound))
	})
	router.NoMethod(func(_ context.Context, c *app.RequestContext) {
		FailRequest(c, kerrors.New(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", errorMethodNotAllowed))
	})
	return router, nil
}

func nativePath(template string) (string, *regexp.Regexp, []string) {
	segments := strings.Split(template, "/")
	pattern := make([]string, len(segments))
	native := make([]string, len(segments))
	fields := make([]string, 0)
	variable := regexp.MustCompile(`\{([^{}]+)\}`)
	for i, segment := range segments {
		matches := variable.FindAllStringSubmatchIndex(segment, -1)
		var part strings.Builder
		previous := 0
		for _, match := range matches {
			part.WriteString(regexp.QuoteMeta(segment[previous:match[0]]))
			part.WriteString(`([^/]+)`)
			fields = append(fields, segment[match[2]:match[3]])
			previous = match[1]
		}
		part.WriteString(regexp.QuoteMeta(segment[previous:]))
		pattern[i] = part.String()
		native[i] = segment
		if len(matches) > 0 || strings.Contains(segment, ":") {
			native[i] = fmt.Sprintf(":segment%d", i)
		}
	}
	return strings.Join(native, "/"), regexp.MustCompile("^" + strings.Join(pattern, "/") + "$"), fields
}

func (r *Router) Register(method, template, operation string, stream bool, handler app.HandlerFunc) {
	path, matcher, fields := nativePath(template)
	key := method + " " + path
	first := len(r.bindings[key]) == 0
	for _, existing := range r.bindings[key] {
		if existing.template == template {
			panic("duplicate protobuf HTTP binding: " + method + " " + template)
		}
	}
	r.bindings[key] = append(r.bindings[key], routeBinding{
		template:  template,
		operation: operation,
		match:     matcher,
		fields:    fields,
		stream:    stream,
		handler:   handler,
	})
	if !first {
		return
	}
	r.Handle(method, path, func(ctx context.Context, c *app.RequestContext) {
		for _, binding := range r.bindings[key] {
			values := binding.match.FindStringSubmatch(string(c.Path()))
			if values == nil {
				continue
			}
			pathValues := make(map[string]string, len(binding.fields))
			for i, field := range binding.fields {
				pathValues[field] = values[i+1]
			}
			c.Set(pathValuesKey, pathValues)
			request, err := http.NewRequestWithContext(ctx, string(c.Method()), string(c.Request.URI().RequestURI()), bytes.NewReader(c.Request.Body()))
			if err != nil {
				FailRequest(c, operatorV1.ErrorAdminBadRequest("invalid request URI: %v", err))
				return
			}
			c.Request.Header.VisitAll(func(key, value []byte) { request.Header.Add(string(key), string(value)) })
			request.RequestURI = string(c.Request.URI().RequestURI())
			request.Host = string(c.Request.Host())
			request.RemoteAddr = c.RemoteAddr().String()
			if string(c.URI().Scheme()) == "https" {
				request.TLS = &tls.ConnectionState{}
			}
			exchange := &httpmeta.Exchange{
				RequestValue:  request,
				Response:      make(http.Header),
				OperationName: binding.operation,
				Route:         binding.template,
			}
			ctx = httpmeta.NewContext(ctx, exchange)
			if r.timeout > 0 && !binding.stream {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, r.timeout)
				defer cancel()
			}
			// A route-specific chain keeps authentication/auditing on generated routes.
			handlers := append(app.HandlersChain{}, r.middleware...)
			handlers = append(handlers, binding.handler)
			c.SetHandlers(handlers)
			c.SetIndex(-1)
			c.Next(ctx)
			for key, values := range exchange.Response {
				if strings.EqualFold(key, "X-Audit-Username") {
					continue
				}
				for _, value := range values {
					c.Response.Header.Add(key, value)
				}
			}
			return
		}
		FailRequest(c, operatorV1.ErrorAdminNotFound(errorRouteNotFound))
	})
}

func RecoverRequest() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		defer func() {
			if value := recover(); value != nil {
				slog.ErrorContext(ctx, "HTTP handler panic", "panic", value, "stack", string(debug.Stack()))
				FailRequest(c, operatorV1.ErrorAdminInternalServerError(errorInternalServer))
			}
		}()
		c.Next(ctx)
	}
}

func FailRequest(c *app.RequestContext, err error) {
	c.Set(requestErrorKey, err)
	c.Abort()
	if started, _ := c.Get(streamStartedKey); started != true {
		encodeAPIError(c, err)
	}
}
func RequestError(c *app.RequestContext) error {
	value, _ := c.Get(requestErrorKey)
	err, _ := value.(error)
	return err
}

func corsMiddleware(cors *conf.Server_REST_CORS) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		origin := string(c.Request.Header.Peek("Origin"))
		if origin != "" {
			allowed := false
			for _, candidate := range cors.GetOrigins() {
				if candidate == origin || candidate == "*" {
					allowed = true
					break
				}
			}
			if allowed {
				c.Response.Header.Add("Vary", "Origin")
				c.Response.Header.Set("Access-Control-Allow-Origin", origin)
				if cors.GetAllowCredentials() {
					c.Response.Header.Set("Access-Control-Allow-Credentials", "true")
				}
				c.Response.Header.Set("Access-Control-Allow-Methods", strings.Join(cors.GetMethods(), ", "))
				headers := append([]string{}, cors.GetHeaders()...)
				headers = append(headers, "X-Tenant-Code")
				c.Response.Header.Set("Access-Control-Allow-Headers", strings.Join(headers, ", "))
			}
			if string(c.Method()) == http.MethodOptions {
				if !allowed {
					FailRequest(c, operatorV1.ErrorAdminForbidden(errorOriginNotAllowed))
					return
				}
				c.SetStatusCode(http.StatusNoContent)
				c.Abort()
				return
			}
		}
		c.Next(ctx)
	}
}
