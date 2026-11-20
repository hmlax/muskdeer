package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hmlax/muskdeer/deer/management/runtimeconfig"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-utils/trans"

	"github.com/hmlax/muskdeer/operator/internal/data"

	"github.com/hmlax/muskdeer/deer/management/constants"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

// startFakeOIDCProvider 起 httptest 假 IdP：discovery / token / userinfo 三端点。
// token 端点校验 code 与 client_secret；userinfo 校验 Bearer 并返回 email。
func startFakeOIDCProvider(t *testing.T, email string) *httptest.Server {
	t.Helper()
	// baseURL 闭包变量：srv.URL 在 Listen 后才可知，handler 请求时才读
	baseURL := ""
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 baseURL,
			"authorization_endpoint": baseURL + "/authorize",
			"token_endpoint":         baseURL + "/token",
			"userinfo_endpoint":      baseURL + "/userinfo",
		})
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.FormValue("code") != "good-code" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"bad code"}`))
			return
		}
		if r.FormValue("client_secret") != "test-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid client"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "fake-at", "token_type": "Bearer"})
	})

	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fake-at" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"email": email, "preferred_username": email})
	})

	srv := httptest.NewServer(mux)
	baseURL = srv.URL
	t.Cleanup(srv.Close)
	return srv
}

// wireSsoConfig 配置Nacos 配置指向假 IdP 并注入 state 缓存（miniredis 在基座里已建）。
func wireSsoConfig(t *testing.T, e *authSvcEnv, fake *httptest.Server) {
	t.Helper()
	e.svc.settings.SSO = runtimeconfig.SSOSettings{
		Issuer:       fake.URL,
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  fake.URL + "/callback",
		Scopes:       "openid email",
	}
	ssoRdb := redis.NewClient(&redis.Options{Addr: e.mr.Addr()})
	e.svc.ssoStateCache = data.NewSsoStateCacheForTest(ssoRdb)
}

func TestSsoLoginInfoDisabledByDefault(t *testing.T) {
	e := newAuthenticationServiceForTest(t)
	resp, err := e.svc.GetSsoLoginInfo(e.ctx, &operatorV1.GetSsoLoginInfoRequest{})
	require.NoError(t, err)
	require.False(t, resp.GetEnabled(), "未配置Nacos 配置时 SSO 应为关闭")

	_, err = e.svc.GetSsoLoginUrl(e.ctx, &operatorV1.GetSsoLoginUrlRequest{})
	require.Error(t, err, "未配置时取登录 URL 应报错")
}

func TestSsoLoginUrlGeneratesState(t *testing.T) {
	e := newAuthenticationServiceForTest(t)
	fake := startFakeOIDCProvider(t, "unused@example.com")
	wireSsoConfig(t, e, fake)

	resp, err := e.svc.GetSsoLoginUrl(e.ctx, &operatorV1.GetSsoLoginUrlRequest{})
	require.NoError(t, err)
	require.Contains(t, resp.GetAuthorizationUrl(), "/authorize", "授权 URL 指向假 IdP")
	require.Contains(t, resp.GetAuthorizationUrl(), "response_type=code")
	require.Contains(t, resp.GetAuthorizationUrl(), "client_id=test-client")
	require.NotEmpty(t, resp.GetState())

	// state 已落缓存：可被取删一次
	ok, err := e.svc.ssoStateCache.Take(e.ctx, resp.GetState())
	require.NoError(t, err)
	require.True(t, ok)
	// 取删后再取应失效
	_, err = e.svc.ssoStateCache.Take(e.ctx, resp.GetState())
	require.Error(t, err, "state 单次有效")
}

func TestSsoLoginHappyPath(t *testing.T) {
	e := newAuthenticationServiceForTest(t)
	const email = "sso-user@example.com"
	fake := startFakeOIDCProvider(t, email)
	wireSsoConfig(t, e, fake)

	// 预置本地用户（email 映射）+ 平台管理员角色（resolveUserAuthority 需要
	// sys:access_backend 权限，与既有登录测试同款装配）
	permID := e.seedBackendAccessPermission(t)
	roleID := e.seedRole(t, nil, operatorV1.Role_SYSTEM, constants.PlatformAdminRoleCode, []uint32{permID}, nil, nil)
	e.seedStubUser(7701, nil, "sso-user", operatorV1.User_NORMAL, []uint32{roleID})
	e.stub.emailToUserID = map[string]uint32{email: 7701}

	urlResp, err := e.svc.GetSsoLoginUrl(e.ctx, &operatorV1.GetSsoLoginUrlRequest{})
	require.NoError(t, err)

	resp, err := e.svc.SsoLogin(e.ctx, &operatorV1.SsoLoginRequest{
		Code:  "good-code",
		State: urlResp.GetState(),
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.GetAccessToken(), "应签发 access token")
	require.Equal(t, operatorV1.TokenType_bearer, resp.GetTokenType())
}

func TestSsoLoginStateReplayRejected(t *testing.T) {
	e := newAuthenticationServiceForTest(t)
	fake := startFakeOIDCProvider(t, "replay-user@example.com")
	wireSsoConfig(t, e, fake)

	const email = "replay-user@example.com"
	permID := e.seedBackendAccessPermission(t)
	roleID := e.seedRole(t, nil, operatorV1.Role_SYSTEM, constants.PlatformAdminRoleCode, []uint32{permID}, nil, nil)
	e.seedStubUser(7710, nil, "replay-user", operatorV1.User_NORMAL, []uint32{roleID})
	e.stub.emailToUserID = map[string]uint32{email: 7710}
	// 假 IdP 的 userinfo 固定返回 startFakeOIDCProvider 注入的 email——这里用同邮箱
	_ = email

	urlResp, err := e.svc.GetSsoLoginUrl(e.ctx, &operatorV1.GetSsoLoginUrlRequest{})
	require.NoError(t, err)

	_, err = e.svc.SsoLogin(e.ctx, &operatorV1.SsoLoginRequest{
		Code:  "good-code",
		State: urlResp.GetState(),
	})
	require.NoError(t, err)

	// 同一 state 重放：取删后已不存在 → 拒绝
	_, err = e.svc.SsoLogin(e.ctx, &operatorV1.SsoLoginRequest{
		Code:  "good-code",
		State: urlResp.GetState(),
	})
	require.Error(t, err, "state 重放应被拒绝")
}

func TestSsoLoginNoLocalAccountRejected(t *testing.T) {
	e := newAuthenticationServiceForTest(t)
	fake := startFakeOIDCProvider(t, "stranger@example.com")
	wireSsoConfig(t, e, fake)
	// 不预置任何 email 映射：IdP 用户在本系统无账号，且 AUTO_CREATE 未开
	e.svc.settings.SSO.AutoCreate = false

	urlResp, err := e.svc.GetSsoLoginUrl(e.ctx, &operatorV1.GetSsoLoginUrlRequest{})
	require.NoError(t, err)

	_, err = e.svc.SsoLogin(e.ctx, &operatorV1.SsoLoginRequest{
		Code:  "good-code",
		State: urlResp.GetState(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no local account")
}

func TestSsoLoginFrozenUserRejected(t *testing.T) {
	e := newAuthenticationServiceForTest(t)
	const email = "frozen@example.com"
	fake := startFakeOIDCProvider(t, email)
	wireSsoConfig(t, e, fake)

	e.stub.emailToUserID = map[string]uint32{email: 7702}
	e.stub.usersByID[7702] = &operatorV1.User{
		Id:       trans.Ptr(uint32(7702)),
		Username: trans.Ptr("frozen"),
		Status:   operatorV1.User_DISABLED.Enum(),
	}

	urlResp, err := e.svc.GetSsoLoginUrl(e.ctx, &operatorV1.GetSsoLoginUrlRequest{})
	require.NoError(t, err)

	_, err = e.svc.SsoLogin(e.ctx, &operatorV1.SsoLoginRequest{
		Code:  "good-code",
		State: urlResp.GetState(),
	})
	require.Error(t, err, "冻结用户 SSO 登录应被拒绝")
}

// TestSsoExchangeCodeBadCode 码不对时 token 端点拒绝 → SsoLogin 401。
func TestSsoExchangeCodeBadCode(t *testing.T) {
	e := newAuthenticationServiceForTest(t)
	fake := startFakeOIDCProvider(t, "unused@example.com")
	wireSsoConfig(t, e, fake)

	state := "s" + time.Now().Format("150405.000000000")
	require.NoError(t, e.svc.ssoStateCache.Put(e.ctx, state))

	_, err := e.svc.SsoLogin(e.ctx, &operatorV1.SsoLoginRequest{
		Code:  "bad-code",
		State: state,
	})
	require.Error(t, err)
}
