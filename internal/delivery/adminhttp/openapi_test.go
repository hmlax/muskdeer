package adminhttp

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"

	"github.com/hmlax/muskdeer/internal/adminopenapi"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestOpenAPIDownloadRequiresCurrentViewerAndReturnsGeneratedBytes(t *testing.T) {
	auth := &openAPIAuth{
		principal: testPrincipal(),
	}
	hertz := openAPIHTTPFixture(t, auth)
	cookie := ut.Header{
		Key:   "Cookie",
		Value: secureSessionCookie + "=" + testSecret(1),
	}
	want, err := adminopenapi.Generate()
	if err != nil {
		t.Fatal(err)
	}
	reply := perform(hertz, "GET", "/openapi.json", "", cookie)
	if reply.Code != 200 || !bytes.Equal(reply.Body.Bytes(), want) || auth.identities != 1 || !auth.bounded {
		t.Fatalf("document was absent, changed or not currently authorized: status %d", reply.Code)
	}
	if string(reply.Header().Peek("Content-Type")) != "application/json; charset=utf-8" || string(reply.Header().Peek("Cache-Control")) != "no-store" {
		t.Fatal("document response lost JSON or no-store boundary")
	}
	auth.failure = domain.ErrUnauthenticated
	reply = perform(hertz, "GET", "/openapi.json", "", cookie)
	if reply.Code != 401 || bytes.Contains(reply.Body.Bytes(), []byte(`"openapi"`)) {
		t.Fatal("stale session downloaded or retained document bytes")
	}
}

func TestOpenAPIDownloadRejectsAmbiguityAndDisabledFeature(t *testing.T) {
	auth := &openAPIAuth{
		principal: testPrincipal(),
	}
	hertz := openAPIHTTPFixture(t, auth)
	for _, test := range []struct {
		method string
		path   string
		body   string
		cookie string
		status int
	}{
		{
			method: "GET",
			path:   "/openapi.json",
			status: 401,
		},
		{
			method: "GET",
			path:   "/openapi.json",
			cookie: secureSessionCookie + "=" + testSecret(1) + "; " + secureSessionCookie + "=" + testSecret(1),
			status: 401,
		},
		{
			method: "GET",
			path:   "/openapi.json?session=forged",
			status: 400,
		},
		{
			method: "GET",
			path:   "/openapi.json",
			body:   "{}",
			status: 400,
		},
		{
			method: "POST",
			path:   "/openapi.json",
			status: 405,
		},
	} {
		if reply := perform(hertz, test.method, test.path, test.body, ut.Header{Key: "Cookie", Value: test.cookie}); reply.Code != test.status {
			t.Fatalf("ambiguous document request status %d, want %d", reply.Code, test.status)
		}
	}
	if auth.identities != 0 {
		t.Fatal("invalid document request reached persistent identity")
	}
	if reply := perform(httpFixture(t, auth), "GET", "/openapi.json", ""); reply.Code != 404 || auth.identities != 0 {
		t.Fatal("query-disabled API exposed documentation")
	}
}

func TestOpenAPIDownloadRejectsPrivateFailuresAndMalformedPrincipals(t *testing.T) {
	auth := &openAPIAuth{
		principal: testPrincipal(),
		failure:   errors.New("private identity database credential"),
	}
	hertz := openAPIHTTPFixture(t, auth)
	cookie := ut.Header{
		Key:   "Cookie",
		Value: secureSessionCookie + "=" + testSecret(1),
	}
	reply := perform(hertz, "GET", "/openapi.json", "", cookie)
	if reply.Code != 503 || strings.Contains(reply.Body.String(), "private") || strings.Contains(reply.Body.String(), `"openapi"`) {
		t.Fatal("identity failure leaked metadata or private errors")
	}
	auth.failure = domain.ErrForbidden
	if reply := perform(hertz, "GET", "/openapi.json", "", cookie); reply.Code != 403 {
		t.Fatal("denied current authority downloaded document")
	}
	auth.failure = nil
	auth.principal.UserID = 0
	if reply := perform(hertz, "GET", "/openapi.json", "", cookie); reply.Code != 503 {
		t.Fatal("invalid identity downloaded document")
	}
}

type openAPIAuth struct {
	domain.AdminAuth
	principal  domain.AdminPrincipal
	failure    error
	identities int
	bounded    bool
}

func (a *openAPIAuth) Identity(ctx context.Context, token string) (domain.AdminPrincipal, error) {
	a.identities++
	deadline, ok := ctx.Deadline()
	a.bounded = ok && time.Until(deadline) <= strategyReadTimeout && ctx.Err() == nil && token == testSecret(1)
	return a.principal, a.failure
}

func openAPIHTTPFixture(t *testing.T, auth domain.AdminAuth) *server.Hertz {
	t.Helper()
	routes, err := New(auth, testOrigin, true)
	if err != nil {
		t.Fatal(err)
	}
	routes.strategies = &strategyHTTPClient{}
	hertz := server.New()
	routes.Register(hertz)
	return hertz
}
