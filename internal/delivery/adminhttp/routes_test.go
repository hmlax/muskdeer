package adminhttp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol"

	"github.com/hmlax/muskdeer/internal/domain"
)

const testOrigin = "https://admin.example.test"

func TestLoginUsesHostOnlyHttpOnlyCookieAndNeverSerializesBearer(t *testing.T) {
	auth := &authFixture{}
	hertz := httpFixture(t, auth)
	response := perform(hertz, "POST", "/api/v1/auth/login", `{"username":"viewer","password":"test-only-password"}`, ut.Header{
		Key:   "Origin",
		Value: testOrigin,
	})
	if response.Code != 200 || auth.logins != 1 {
		t.Fatalf("login status=%d calls=%d body=%s", response.Code, auth.logins, response.Body.String())
	}
	if strings.Contains(response.Body.String(), testSecret(1)) || strings.Contains(response.Body.String(), testSecret(2)) {
		t.Fatal("login body exposed a bearer or CSRF secret")
	}
	var session, csrf string
	response.Header().VisitAllCookie(func(key, value []byte) {
		switch string(key) {
		case secureSessionCookie:
			session = string(value)
		case secureCSRFCookie:
			csrf = string(value)
		}
	})
	var sessionCookie, csrfCookie protocol.Cookie
	if err := sessionCookie.Parse(session); err != nil {
		t.Fatal(err)
	}
	if err := csrfCookie.Parse(csrf); err != nil {
		t.Fatal(err)
	}
	if !sessionCookie.HTTPOnly() || !sessionCookie.Secure() || sessionCookie.SameSite() != protocol.CookieSameSiteStrictMode || string(sessionCookie.Path()) != "/" || len(sessionCookie.Domain()) != 0 {
		t.Fatal("session cookie boundary attributes missing")
	}
	if csrf == "" || csrfCookie.HTTPOnly() || !csrfCookie.Secure() {
		t.Fatal("independent CSRF cookie missing or unreadable")
	}
	if string(response.Header().Peek("Cache-Control")) != "no-store" {
		t.Fatal("session response was cacheable")
	}
}

func TestLoginRejectsOriginSpoofingAndAmbiguousBodiesBeforeCredentials(t *testing.T) {
	for _, body := range []string{
		`{"username":"viewer","username":"admin","password":"test-only-password"}`,
		`{"username":"viewer","password":"test-only-password","role":"admin"}`,
		`{"username":null,"password":"test-only-password"}`,
		`{"username":"viewer","password":123456789012}`,
		`{"username":"viewer","password":"test-only-password"} {}`,
		`{"username":"viewer","password":"short"}`,
		strings.Repeat(" ", maximumLoginBytes+1),
	} {
		auth := &authFixture{}
		response := perform(httpFixture(t, auth), "POST", "/api/v1/auth/login", body, ut.Header{
			Key:   "Origin",
			Value: testOrigin,
		})
		if response.Code != 400 || auth.logins != 0 {
			t.Fatal("ambiguous login reached credentials")
		}
	}
	for _, origin := range []string{"", "https://outside.example.test", testOrigin + "/", "null"} {
		auth := &authFixture{}
		response := perform(httpFixture(t, auth), "POST", "/api/v1/auth/login", `{"username":"viewer","password":"test-only-password"}`, ut.Header{
			Key:   "Origin",
			Value: origin,
		}, ut.Header{
			Key:   "X-Forwarded-Host",
			Value: "admin.example.test",
		})
		if response.Code != 403 || auth.logins != 0 {
			t.Fatal("forwarded metadata or untrusted origin authorized a login")
		}
	}
}

func TestSessionCookiesCsrfErrorsAndMissingAPIRoutesStayTyped(t *testing.T) {
	auth := &authFixture{}
	hertz := httpFixture(t, auth)
	cookie := secureSessionCookie + "=" + testSecret(1)
	response := perform(hertz, "GET", "/api/v1/auth/session", "", ut.Header{
		Key:   "Cookie",
		Value: cookie,
	})
	var session sessionReply
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || auth.identities != 1 || session.Session.Role != domain.AdminRoleViewer {
		t.Fatal("authenticated identity did not reach typed session view")
	}
	response = perform(hertz, "GET", "/api/v1/auth/session", "", ut.Header{
		Key:   "Cookie",
		Value: cookie + "; " + cookie,
	})
	if response.Code != 401 || auth.identities != 1 {
		t.Fatal("ambiguous bearer cookie reached identity")
	}
	response = perform(hertz, "POST", "/api/v1/auth/logout", "", ut.Header{
		Key:   "Cookie",
		Value: cookie,
	}, ut.Header{
		Key:   "Origin",
		Value: testOrigin,
	})
	if response.Code != 403 || auth.logouts != 0 {
		t.Fatal("logout bypassed CSRF")
	}
	response = perform(hertz, "POST", "/api/v1/auth/logout", "", ut.Header{
		Key:   "Cookie",
		Value: cookie,
	}, ut.Header{
		Key:   "Origin",
		Value: testOrigin,
	}, ut.Header{
		Key:   CSRFHeader,
		Value: testSecret(2),
	})
	if response.Code != 204 || auth.logouts != 1 {
		t.Fatal("verified logout did not clear session")
	}
	auth.failure = errors.New("private database bearer password")
	response = perform(hertz, "GET", "/api/v1/auth/session", "", ut.Header{
		Key:   "Cookie",
		Value: cookie,
	})
	if response.Code != 503 || strings.Contains(response.Body.String(), "private") {
		t.Fatal("dependency error escaped or was hidden as unauthenticated")
	}
	response = perform(hertz, "GET", "/api/v1/not-present", "")
	var failure errorReply
	if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil {
		t.Fatal(err)
	}
	if response.Code != 404 || failure.Code != codeNotFound {
		t.Fatal("missing API route was swallowed by another route")
	}
}

type authFixture struct {
	domain.AdminAuth
	logins     int
	identities int
	logouts    int
	failure    error
}

func (a *authFixture) Login(context.Context, string, string) (domain.AdminLogin, error) {
	a.logins++
	return domain.AdminLogin{
		Principal: testPrincipal(),
		Token:     testSecret(1),
		CSRF:      testSecret(2),
	}, a.failure
}

func (a *authFixture) Identity(context.Context, string) (domain.AdminPrincipal, error) {
	a.identities++
	return testPrincipal(), a.failure
}
func (a *authFixture) Logout(_ context.Context, token, csrf string) error {
	if token != testSecret(1) || csrf != testSecret(2) {
		return domain.ErrForbidden
	}
	a.logouts++
	return a.failure
}

func testPrincipal() domain.AdminPrincipal {
	return domain.AdminPrincipal{
		UserID:    1,
		Username:  "viewer",
		Role:      domain.AdminRoleViewer,
		SessionID: 1,
		ExpiresAt: time.Now().UTC().Add(domain.AdminSessionTTL),
	}
}

func testSecret(value byte) string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{value}, 32))
}

func httpFixture(t *testing.T, auth domain.AdminAuth) *server.Hertz {
	t.Helper()
	routes, err := New(auth, testOrigin, true)
	if err != nil {
		t.Fatal(err)
	}
	hertz := server.New()
	routes.Register(hertz)
	return hertz
}

func perform(hertz *server.Hertz, method, path, body string, headers ...ut.Header) *ut.ResponseRecorder {
	base := []ut.Header{{
		Key:   "Host",
		Value: "admin.example.test",
	}, {
		Key:   "Content-Type",
		Value: "application/json",
	}}
	return ut.PerformRequest(hertz.Engine, method, testOrigin+path, &ut.Body{
		Body: strings.NewReader(body),
		Len:  len(body),
	}, append(base, headers...)...)
}
