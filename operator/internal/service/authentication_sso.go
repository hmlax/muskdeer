package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/hmlax/muskdeer/deer/management/portalscope"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"

	"github.com/tx7do/go-utils/id"
	"github.com/tx7do/go-utils/trans"
)

// ==== OIDC SSO 配置（Nacos runtime.sso，opt-in）====

const (
	ssoDiscoveryTTL   = time.Hour
	ssoDefaultScopes  = "openid email profile"
	ssoStateTTLClient = 10 * time.Minute
)

// ssoOIDCConfig Nacos 中的 SSO 配置；Enabled 齐备即开启。
type ssoOIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       string
	AutoCreate   bool
	TenantCode   string
}

func (c *ssoOIDCConfig) Enabled() bool {
	return c.Issuer != "" && c.ClientID != "" && c.ClientSecret != "" && c.RedirectURL != ""
}

// ssoOIDCDiscovery OIDC discovery 文档中本流程用到的三个端点。
type ssoOIDCDiscovery struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
}

// ssoDiscoveryCache 进程内 discovery 缓存（issuer → 文档 + 过期时间）。
// 单例内存缓存即可：discovery 指纹变化极罕见，过期后下次登录重拉。
var (
	ssoDiscoveryMu     sync.Mutex
	ssoDiscoveryCache  = map[string]ssoOIDCDiscovery{}
	ssoDiscoveryExpiry = map[string]time.Time{}
)

// ssoHTTPClient OIDC 出站 HTTP 客户端；测试注入 httptest 假 IdP。
var ssoHTTPClient = &http.Client{Timeout: 15 * time.Second}

// ssoConfig reads the service's startup configuration from Nacos.
func (s *AuthenticationService) ssoConfig() ssoOIDCConfig {
	cfg := s.settings.SSO
	return ssoOIDCConfig{
		Issuer:       strings.TrimSpace(cfg.Issuer),
		ClientID:     strings.TrimSpace(cfg.ClientID),
		ClientSecret: strings.TrimSpace(cfg.ClientSecret),
		RedirectURL:  strings.TrimSpace(cfg.RedirectURL),
		Scopes:       strings.TrimSpace(firstNonEmpty(cfg.Scopes, ssoDefaultScopes)),
		AutoCreate:   cfg.AutoCreate,
		TenantCode:   strings.TrimSpace(cfg.TenantCode),
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// ssoFetchDiscovery 拉（带缓存）OIDC discovery 文档。
func (s *AuthenticationService) ssoFetchDiscovery(ctx context.Context, issuer string) (*ssoOIDCDiscovery, error) {
	ssoDiscoveryMu.Lock()
	if exp, ok := ssoDiscoveryExpiry[issuer]; ok && time.Now().Before(exp) {
		doc := ssoDiscoveryCache[issuer]
		ssoDiscoveryMu.Unlock()
		return &doc, nil
	}
	ssoDiscoveryMu.Unlock()

	wellKnown := strings.TrimSuffix(issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnown, nil)
	if err != nil {
		return nil, fmt.Errorf("build discovery request failed: %w", err)
	}
	resp, err := ssoHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch discovery failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("discovery returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var doc ssoOIDCDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode discovery failed: %w", err)
	}
	if doc.AuthorizationEndpoint == "" || doc.TokenEndpoint == "" || doc.UserinfoEndpoint == "" {
		return nil, fmt.Errorf("discovery document missing endpoints")
	}

	ssoDiscoveryMu.Lock()
	ssoDiscoveryCache[issuer] = doc
	ssoDiscoveryExpiry[issuer] = time.Now().Add(ssoDiscoveryTTL)
	ssoDiscoveryMu.Unlock()

	return &doc, nil
}

// GetSsoLoginInfo SSO 能力开关（登录页按钮显隐）。
func (s *AuthenticationService) GetSsoLoginInfo(ctx context.Context, _ *operatorV1.GetSsoLoginInfoRequest) (*operatorV1.GetSsoLoginInfoResponse, error) {
	if err := portalscope.CheckTenant(ctx, 0); err != nil {
		return &operatorV1.GetSsoLoginInfoResponse{Enabled: false}, nil
	}
	cfg := s.ssoConfig()
	return &operatorV1.GetSsoLoginInfoResponse{Enabled: cfg.Enabled()}, nil
}

// GetSsoLoginUrl 生成 OIDC 授权跳转 URL，state 落 Redis（10 分钟单次有效）。
func (s *AuthenticationService) GetSsoLoginUrl(ctx context.Context, _ *operatorV1.GetSsoLoginUrlRequest) (*operatorV1.GetSsoLoginUrlResponse, error) {
	if err := portalscope.CheckTenant(ctx, 0); err != nil {
		return nil, err
	}
	cfg := s.ssoConfig()
	if !cfg.Enabled() {
		return nil, operatorV1.ErrorAuthenticationBadRequest("sso is not configured")
	}

	discovery, err := s.ssoFetchDiscovery(ctx, cfg.Issuer)
	if err != nil {
		s.log.Errorf(ctx, "sso login url: discovery failed: %s", err.Error())
		return nil, operatorV1.ErrorAuthenticationInternalServerError("fetch sso discovery failed")
	}

	state := id.NewGUIDv4(false)
	if s.ssoStateCache == nil {
		return nil, operatorV1.ErrorAuthenticationInternalServerError("sso state cache is not wired")
	}
	if err := s.ssoStateCache.Put(ctx, state); err != nil {
		return nil, operatorV1.ErrorAuthenticationInternalServerError("store sso state failed")
	}

	authURL := fmt.Sprintf("%s?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&state=%s",
		discovery.AuthorizationEndpoint,
		url.QueryEscape(cfg.ClientID),
		url.QueryEscape(cfg.RedirectURL),
		url.QueryEscape(cfg.Scopes),
		url.QueryEscape(state),
	)

	return &operatorV1.GetSsoLoginUrlResponse{
		AuthorizationUrl: authURL,
		State:            state,
	}, nil
}

// SsoLogin OIDC 回调换本系统令牌。
//
// 流程：state 验证取删 → code 换 access_token → userinfo 取 email →
// 按邮箱定位用户（未找到且 SSO_AUTO_CREATE 时自动预置）→ resolveUserAuthority →
// 签发 JWT（refresh token 走 HttpOnly Cookie，与密码登录同形）。
// V1 边界：SSO 登录不触发本系统 TOTP MFA 闸门（认证已在 IdP 完成），
// 也不走密码登录的限流/登录策略（无口令攻击面）。
func (s *AuthenticationService) SsoLogin(ctx context.Context, req *operatorV1.SsoLoginRequest) (*operatorV1.LoginResponse, error) {
	if err := portalscope.CheckTenant(ctx, 0); err != nil {
		return nil, err
	}
	cfg := s.ssoConfig()
	if !cfg.Enabled() {
		return nil, operatorV1.ErrorAuthenticationBadRequest("sso is not configured")
	}
	if req.GetCode() == "" || req.GetState() == "" {
		return nil, operatorV1.ErrorAuthenticationBadRequest("code and state are required")
	}
	if s.ssoStateCache == nil {
		return nil, operatorV1.ErrorAuthenticationInternalServerError("sso state cache is not wired")
	}

	// 与 doGrantTypePassword 同款：登录链路的用户查询需要绕过隐私层
	// （此时无 viewer，TenantPrivacy 会拒绝查询）。SSO 是登录入口，同权。
	ctx = s.resetContextForLogin(ctx)

	// 1. state 单次验证取删（防 CSRF 与回调重放）
	ok, err := s.ssoStateCache.Take(ctx, req.GetState())
	if err != nil {
		return nil, operatorV1.ErrorAuthenticationInternalServerError("validate sso state failed")
	}
	if !ok {
		return nil, operatorV1.ErrorAuthenticationUnauthorized("sso state invalid or expired")
	}

	discovery, err := s.ssoFetchDiscovery(ctx, cfg.Issuer)
	if err != nil {
		s.log.Errorf(ctx, "sso login: discovery failed: %s", err.Error())
		return nil, operatorV1.ErrorAuthenticationInternalServerError("fetch sso discovery failed")
	}

	// 2. code 换 access_token
	accessToken, err := s.ssoExchangeCode(ctx, discovery.TokenEndpoint, cfg, req.GetCode())
	if err != nil {
		s.log.Errorf(ctx, "sso login: exchange code failed: %s", err.Error())
		return nil, operatorV1.ErrorAuthenticationUnauthorized("sso code exchange failed")
	}

	// 3. userinfo 取身份（email 为映射锚）
	claims, err := s.ssoFetchUserinfo(ctx, discovery.UserinfoEndpoint, accessToken)
	if err != nil {
		s.log.Errorf(ctx, "sso login: fetch userinfo failed: %s", err.Error())
		return nil, operatorV1.ErrorAuthenticationUnauthorized("sso userinfo failed")
	}
	email := strings.TrimSpace(claims["email"])
	if email == "" {
		return nil, operatorV1.ErrorAuthenticationUnauthorized("sso userinfo has no email claim")
	}

	// 4. 定位/预置用户
	tenantID := uint32(0)
	if cfg.TenantCode != "" {
		tenant, terr := s.tenantRepo.Get(ctx, &operatorV1.GetTenantRequest{
			QueryBy: &operatorV1.GetTenantRequest_Code{Code: cfg.TenantCode},
		})
		if terr != nil || tenant == nil || tenant.GetId() == 0 {
			s.log.Errorf(ctx, "sso login: tenant code [%s] not found", cfg.TenantCode)
			return nil, operatorV1.ErrorAuthenticationInternalServerError("sso tenant misconfigured")
		}
		tenantID = tenant.GetId()
	}

	username, userID, ferr := s.userRepo.FindUsernameByIdentifier(ctx, tenantID, email)
	if ferr != nil {
		s.log.Errorf(ctx, "sso login: find user by email failed: %s", ferr.Error())
		return nil, operatorV1.ErrorAuthenticationInternalServerError("find user failed")
	}
	if userID == 0 {
		if !cfg.AutoCreate {
			return nil, operatorV1.ErrorAuthenticationForbidden("no local account matches the sso identity; ask an administrator to create one")
		}
		created, cerr := s.ssoAutoCreateUser(ctx, tenantID, email)
		if cerr != nil {
			s.log.Errorf(ctx, "sso login: auto create user failed: %s", cerr.Error())
			return nil, operatorV1.ErrorAuthenticationInternalServerError("auto create user failed")
		}
		userID = created.GetId()
		username = created.GetUsername()
	}

	user, uerr := s.userRepo.Get(ctx, &operatorV1.GetUserRequest{
		QueryBy: &operatorV1.GetUserRequest_Id{Id: userID},
	})
	if uerr != nil || user == nil {
		return nil, operatorV1.ErrorAuthenticationUnauthorized("sso user not found")
	}
	if sso := user.GetStatus(); sso != operatorV1.User_NORMAL {
		return nil, operatorV1.ErrorAuthenticationForbidden("account is frozen")
	}

	// 5. 权限解析 + 签发（与密码登录同一套）
	tokenPayload := &operatorV1.UserTokenPayload{
		UserId:   user.GetId(),
		TenantId: user.TenantId,
		Username: user.Username,
		ClientId: trans.Ptr("sso"),
	}
	if err := s.resolveUserAuthority(ctx, user, tokenPayload); err != nil {
		s.log.Errorf(ctx, "sso login: resolve authority failed for user [%d]: %s", user.GetId(), err.Error())
		return nil, err
	}

	accessToken, refreshToken, terr := s.authenticator.CreateUserToken(ctx, operatorV1.ClientType_admin, tokenPayload)
	if terr != nil {
		return nil, terr
	}

	recordSessionMeta(ctx, s.log, s.authenticator, operatorV1.ClientType_admin, tokenPayload)
	recordUserLastLogin(ctx, s.log, s.userRepo, user.GetId(), "")

	refreshExpiresIn := int64(s.authenticator.GetRefreshTokenExpires(operatorV1.ClientType_admin).Seconds())
	setRefreshCookies(ctx, refreshToken, refreshExpiresIn)

	s.log.Infof(ctx, "sso login: user [%s] (id=%d) logged in via oidc (%s)", username, user.GetId(), cfg.Issuer)

	return &operatorV1.LoginResponse{
		TokenType:   operatorV1.TokenType_bearer,
		AccessToken: accessToken,
		ExpiresIn:   int64(s.authenticator.GetAccessTokenExpires(operatorV1.ClientType_admin).Seconds()),
	}, nil
}

// ssoExchangeCode 在 IdP token 端点用授权码换 access_token（client_secret_post）。
func (s *AuthenticationService) ssoExchangeCode(ctx context.Context, tokenEndpoint string, cfg ssoOIDCConfig, code string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", cfg.RedirectURL)
	form.Set("client_id", cfg.ClientID)
	form.Set("client_secret", cfg.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := ssoHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("decode token response failed: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("token response has no access_token")
	}
	return tokenResp.AccessToken, nil
}

// ssoFetchUserinfo 携 access_token 调 userinfo 端点，返回 claims。
func (s *AuthenticationService) ssoFetchUserinfo(ctx context.Context, userinfoEndpoint, accessToken string) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userinfoEndpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := ssoHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var raw map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode userinfo failed: %w", err)
	}
	claims := make(map[string]string, len(raw))
	for k, v := range raw {
		switch tv := v.(type) {
		case string:
			claims[k] = tv
		case json.Number:
			claims[k] = tv.String()
		case bool:
			claims[k] = fmt.Sprintf("%v", tv)
		default:
			// 嵌套结构（如 email_verified 之外的复杂 claim）不进平铺映射
		}
	}
	return claims, nil
}

// ssoAutoCreateUser 按邮箱预置用户：username 取邮箱本地部分，无密码凭证
// （管理员设置密码前该账号无法密码登录），状态 ON。
func (s *AuthenticationService) ssoAutoCreateUser(ctx context.Context, tenantID uint32, email string) (*operatorV1.User, error) {
	local := email
	if at := strings.Index(local, "@"); at > 0 {
		local = local[:at]
	}
	username := fmt.Sprintf("sso_%s_%s", local, id.NewGUIDv4(false)[:8])

	return s.userRepo.Create(ctx, &operatorV1.CreateUserRequest{
		Data: &operatorV1.User{
			Username: trans.Ptr(username),
			Email:    trans.Ptr(email),
			TenantId: trans.Ptr(tenantID),
			Status:   operatorV1.User_NORMAL.Enum(),
		},
	})
}
