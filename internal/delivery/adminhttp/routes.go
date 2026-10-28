package adminhttp

import (
	"context"
	"encoding/base64"
	"errors"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// CSRFHeader binds a management write to the independently delivered secret.
const CSRFHeader = "X-CSRF-Token"

const (
	maximumAdminBodyBytes   = 64 * 1024
	messageInvalidRequest   = "invalid management request"
	messageNotFound         = "management resource not found"
	messageMethodNotAllowed = "method not allowed"
	codeInvalidRequest      = "INVALID_REQUEST"
	codeUnauthenticated     = "UNAUTHENTICATED"
	codeForbidden           = "FORBIDDEN"
	codeUnavailable         = "UNAVAILABLE"
	codeNotFound            = "NOT_FOUND"
	codeMethodNotAllowed    = "METHOD_NOT_ALLOWED"
	codeConflict            = "CONFLICT"
	secureSessionCookie     = "__Host-muskdeer_session"
	secureCSRFCookie        = "__Host-muskdeer_csrf"
	localSessionCookie      = "muskdeer_session"
	localCSRFCookie         = "muskdeer_csrf"
)

type errorReply struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type sessionView struct {
	UserID                  string           `json:"user_id"`
	Username                string           `json:"username"`
	Role                    domain.AdminRole `json:"role"`
	SessionID               string           `json:"session_id"`
	ExpiresAtMS             string           `json:"expires_at_ms"`
	StrategyQueriesEnabled  bool             `json:"strategy_queries_enabled"`
	StrategyControlsEnabled bool             `json:"strategy_controls_enabled"`
}

type sessionReply struct {
	Session sessionView `json:"session"`
}

// Routes owns immutable delivery values and borrows the sole auth instance.
type Routes struct {
	auth          domain.AdminAuth
	strategies    strategyv1.StrategyManagementClient
	catalog       strategyv1.StrategyCatalogClient
	controls      strategyv1.StrategyControlClient
	origin        string
	host          string
	secure        bool
	sessionCookie string
	csrfCookie    string
}

// New constructs authentication routes from an already resolved fixed origin.
func New(auth domain.AdminAuth, origin string, secure bool) (*Routes, error) {
	if auth == nil {
		return nil, ErrInvalidConfig
	}
	parsed, err := parseOrigin(origin, !secure)
	if err != nil || parsed.Port() == "0" || (parsed.Scheme == "https") != secure {
		return nil, ErrInvalidConfig
	}
	result := &Routes{
		auth:          auth,
		origin:        origin,
		host:          parsed.Host,
		secure:        secure,
		sessionCookie: localSessionCookie,
		csrfCookie:    localCSRFCookie,
	}
	if secure {
		result.sessionCookie, result.csrfCookie = secureSessionCookie, secureCSRFCookie
	}
	return result, nil
}

// Register configures only the API server before its Deer-owned Run starts.
func (r *Routes) Register(hertz *server.Hertz) {
	hertz.GetOptions().MaxRequestBodySize = maximumAdminBodyBytes
	hertz.GetOptions().HandleMethodNotAllowed = true
	hertz.Use(r.headers)
	r.pages(hertz)
	hertz.POST("/api/v1/auth/login", r.login)
	hertz.GET("/api/v1/auth/session", r.session)
	hertz.POST("/api/v1/auth/logout", r.logout)
	if r.strategies != nil {
		hertz.GET("/api/v1/strategies", r.listStrategies)
		hertz.GET("/api/v1/strategies/:strategyID", r.getStrategy)
		hertz.GET("/api/v1/strategies/:strategyID/versions", r.listStrategyVersions)
	}
	if r.controls != nil {
		hertz.POST("/api/v1/strategies/:strategyID/control", r.controlStrategy)
		hertz.GET("/api/v1/operations/:operationID", r.getOperation)
	}
	if r.catalog != nil {
		hertz.GET("/api/v1/accounts", r.listAccounts)
		hertz.GET("/api/v1/instruments", r.listInstruments)
		hertz.GET("/api/v1/instruments/:instrumentID/market-feeds", r.listMarketFeeds)
	}
	hertz.NoRoute(func(_ context.Context, request *app.RequestContext) {
		if strings.HasPrefix(string(request.Path()), "/api/") {
			writeError(request, http.StatusNotFound, codeNotFound, messageNotFound)
			return
		}
		request.String(http.StatusNotFound, "not found")
	})
	hertz.NoMethod(func(_ context.Context, request *app.RequestContext) {
		writeError(request, http.StatusMethodNotAllowed, codeMethodNotAllowed, messageMethodNotAllowed)
	})
}

func (r *Routes) headers(ctx context.Context, request *app.RequestContext) {
	request.Response.Header.Set("Cache-Control", "no-store")
	request.Response.Header.Set("Vary", "Cookie")
	request.Response.Header.Set("X-Content-Type-Options", "nosniff")
	request.Response.Header.Set("Referrer-Policy", "no-referrer")
	request.Response.Header.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	if string(request.Host()) != r.host {
		writeError(request, http.StatusForbidden, codeForbidden, domain.MessageForbidden)
		request.Abort()
		return
	}
	request.Next(ctx)
}

func (r *Routes) sameOrigin(request *app.RequestContext) bool {
	values := request.Request.Header.PeekAll("Origin")
	return len(values) == 1 && string(values[0]) == r.origin
}

func (r *Routes) login(ctx context.Context, request *app.RequestContext) {
	if !r.sameOrigin(request) {
		writeError(request, http.StatusForbidden, codeForbidden, domain.MessageForbidden)
		return
	}
	if !loginContentType(request) {
		writeError(request, http.StatusBadRequest, codeInvalidRequest, messageInvalidRequest)
		return
	}
	input, err := decodeLogin(request.Request.Body())
	if err != nil {
		writeError(request, http.StatusBadRequest, codeInvalidRequest, messageInvalidRequest)
		return
	}
	login, err := r.auth.Login(ctx, input.Username, input.Password)
	if err != nil {
		r.failure(request, err)
		return
	}
	view, valid := principalView(login.Principal)
	if !valid || !cookieSecret(login.Token) || !cookieSecret(login.CSRF) || login.Token == login.CSRF {
		r.failure(request, domain.ErrAuthUnavailable)
		return
	}
	r.setCookies(request, login.Token, login.CSRF, int(domain.AdminSessionTTL/time.Second))
	view.StrategyQueriesEnabled = r.strategies != nil
	view.StrategyControlsEnabled = r.controls != nil
	request.JSON(http.StatusOK, sessionReply{Session: view})
}

func (r *Routes) session(ctx context.Context, request *app.RequestContext) {
	token, valid := singleCookie(request, r.sessionCookie)
	if !valid {
		r.failure(request, domain.ErrUnauthenticated)
		return
	}
	principal, err := r.auth.Identity(ctx, token)
	if err != nil {
		r.failure(request, err)
		return
	}
	view, valid := principalView(principal)
	if !valid {
		r.failure(request, domain.ErrAuthUnavailable)
		return
	}
	view.StrategyQueriesEnabled = r.strategies != nil
	view.StrategyControlsEnabled = r.controls != nil
	request.JSON(http.StatusOK, sessionReply{Session: view})
}

func (r *Routes) logout(ctx context.Context, request *app.RequestContext) {
	if !r.sameOrigin(request) {
		writeError(request, http.StatusForbidden, codeForbidden, domain.MessageForbidden)
		return
	}
	token, valid := singleCookie(request, r.sessionCookie)
	if !valid {
		r.failure(request, domain.ErrUnauthenticated)
		return
	}
	proof := request.Request.Header.PeekAll(CSRFHeader)
	if len(proof) != 1 || !cookieSecret(string(proof[0])) {
		r.failure(request, domain.ErrForbidden)
		return
	}
	if err := r.auth.Logout(ctx, token, string(proof[0])); err != nil && !errors.Is(err, domain.ErrUnauthenticated) {
		r.failure(request, err)
		return
	}
	r.setCookies(request, "", "", -1)
	request.Status(http.StatusNoContent)
}

func (r *Routes) setCookies(request *app.RequestContext, token, csrf string, age int) {
	request.SetCookie(r.sessionCookie, token, age, "/", "", protocol.CookieSameSiteStrictMode, r.secure, true)
	request.SetCookie(r.csrfCookie, csrf, age, "/", "", protocol.CookieSameSiteStrictMode, r.secure, false)
}

func (r *Routes) failure(request *app.RequestContext, err error) {
	switch {
	case errors.Is(err, domain.ErrUnauthenticated):
		r.setCookies(request, "", "", -1)
		writeError(request, http.StatusUnauthorized, codeUnauthenticated, domain.MessageUnauthenticated)
	case errors.Is(err, domain.ErrForbidden):
		writeError(request, http.StatusForbidden, codeForbidden, domain.MessageForbidden)
	default:
		writeError(request, http.StatusServiceUnavailable, codeUnavailable, domain.MessageAuthUnavailable)
	}
}

func writeError(request *app.RequestContext, status int, code, message string) {
	request.JSON(status, errorReply{
		Code:    code,
		Message: message,
	})
}

func loginContentType(request *app.RequestContext) bool {
	values := request.Request.Header.PeekAll("Content-Type")
	if len(values) != 1 || len(values[0]) > 128 {
		return false
	}
	contentType, parameters, err := mime.ParseMediaType(string(values[0]))
	return err == nil && contentType == "application/json" && (parameters["charset"] == "" || parameters["charset"] == "utf-8")
}

func singleCookie(request *app.RequestContext, name string) (string, bool) {
	var result string
	count := 0
	request.VisitAllCookie(func(key, value []byte) {
		if string(key) == name {
			count++
			result = string(value)
		}
	})
	return result, count == 1 && cookieSecret(result)
}

func cookieSecret(value string) bool {
	if len(value) != 43 {
		return false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	return err == nil && len(decoded) == 32 && base64.RawURLEncoding.EncodeToString(decoded) == value
}

func principalView(value domain.AdminPrincipal) (sessionView, bool) {
	if value.UserID <= 0 || value.SessionID <= 0 || value.Username == "" || len(value.Username) > 64 || !value.Role.Allows(value.Role) || value.ExpiresAt.UnixMilli() <= 0 {
		return sessionView{}, false
	}
	return sessionView{
		UserID:      strconv.FormatInt(value.UserID, 10),
		Username:    value.Username,
		Role:        value.Role,
		SessionID:   strconv.FormatInt(value.SessionID, 10),
		ExpiresAtMS: strconv.FormatInt(value.ExpiresAt.UnixMilli(), 10),
	}, true
}
