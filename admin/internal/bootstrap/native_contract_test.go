package bootstrap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	conf "github.com/tx7do/kratos-bootstrap/api/gen/go/conf/v1"

	apiV1 "github.com/hmlax/muskdeer/admin/biz/model/admin/v1"
	"github.com/hmlax/muskdeer/deer/management/httpmeta"
)

func TestNativeBindingPreservesBodyQueriesPathAndMasks(t *testing.T) {
	router := contractRouter(t)
	var observed apiV1.UpdateUserRequest
	router.Register(http.MethodPut, "/admin/v1/users/{id}", "/test.User/Update", false, func(_ context.Context, c *app.RequestContext) {
		if err := bindRequest(c, &observed, "*"); err != nil {
			failRequest(c, err)
			return
		}
		c.SetStatusCode(200)
	})
	request := httptest.NewRequest(http.MethodPut, "/admin/v1/users/9?updateMask=username,realname", strings.NewReader(`{"id":999,"data":{"username":"alice"}}`))
	response := httptest.NewRecorder()
	serveNative(router, response, request)
	if response.Code != 200 || observed.GetId() != 9 || observed.GetData().GetUsername() != "alice" || strings.Join(observed.GetUpdateMask().GetPaths(), ",") != "username,realname" {
		t.Fatalf("native update binding = %d %s %v", response.Code, response.Body.String(), &observed)
	}
	var page apiV1.PagingRequest
	router.Register(http.MethodGet, "/paging", "/test.Page/List", false, func(_ context.Context, c *app.RequestContext) {
		if err := bindRequest(c, &page, ""); err != nil {
			failRequest(c, err)
			return
		}
	})
	response = httptest.NewRecorder()
	serveNative(router, response, httptest.NewRequest(http.MethodGet, "/paging?page_size=15&noPaging=true&filterExpr=%7B%22conditions%22%3A%5B%7B%22field%22%3A%22username%22%2C%22value%22%3A%22alice%22%7D%5D%7D", nil))
	if response.Code != 200 || page.GetPageSize() != 15 || !page.GetNoPaging() || len(page.GetFilterExpr().GetConditions()) != 1 {
		t.Fatalf("pagination binding = %d %s %v", response.Code, response.Body.String(), &page)
	}
}
func TestNativeLiteralSuffixCannotMatchAnotherAction(t *testing.T) {
	router := contractRouter(t)
	called := false
	router.Register(http.MethodGet, "/items:export", "/test.Item/Export", false, func(context.Context, *app.RequestContext) { called = true })
	response := httptest.NewRecorder()
	serveNative(router, response, httptest.NewRequest(http.MethodGet, "/items:delete", nil))
	if response.Code != 404 || called {
		t.Fatalf("unregistered action reached export: %d", response.Code)
	}
	response = httptest.NewRecorder()
	serveNative(router, response, httptest.NewRequest(http.MethodGet, "/items:export", nil))
	if response.Code != 200 || !called {
		t.Fatalf("registered export failed: %d", response.Code)
	}
}

func TestHZBindingPreservesPathParameterSuffixAndAuthorizationMetadata(t *testing.T) {
	router := contractRouter(t)
	calls := 0
	router.RegisterHZ(http.MethodGet, "/items/:id:run", hzBinding{
		Operation: "/test.Item/Run",
		Handler: func(ctx context.Context, c *app.RequestContext) {
			var request apiV1.GetUserRequest
			if err := bindRequest(c, &request, ""); err != nil {
				failRequest(c, err)
				return
			}
			exchange, ok := httpmeta.FromContext(ctx)
			if !ok || exchange.PathTemplate() != "/items/{id}:run" || exchange.Operation() != "/test.Item/Run" || request.GetId() != 9 {
				t.Fatalf("hz route lost its typed path or authorization metadata: %v", &request)
			}
			calls++
		},
	})
	for _, path := range []string{"/items/9:run", "/items/9:delete"} {
		response := httptest.NewRecorder()
		serveNative(router, response, httptest.NewRequest(http.MethodGet, path, nil))
		expected := http.StatusOK
		if strings.HasSuffix(path, ":delete") {
			expected = http.StatusNotFound
		}
		if response.Code != expected {
			t.Fatalf("GET %s returned %d", path, response.Code)
		}
	}
	if calls != 1 {
		t.Fatalf("hz literal suffix matched an unregistered action: calls=%d", calls)
	}
}

func TestNativeRequestMetadataPreservesTraceAndClientAddress(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/admin/v1/users", nil)
	request.RemoteAddr = "192.0.2.10:1234"
	request.Header.Set("Traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
	request.Header.Set("X-Real-IP", "203.0.113.8")
	ctx := httpmeta.NewContext(t.Context(), &httpmeta.Exchange{RequestValue: request})
	if traceIDFromContext(ctx) != "0af7651916cd43dd8448eb211c80319c" || clientIPFromContext(ctx) != "203.0.113.8" {
		t.Fatal("request audit correlation metadata was lost")
	}
}

func TestNativeAuditMetadataPreservesRequestTarget(t *testing.T) {
	router := contractRouter(t)
	var observed *http.Request
	router.Register(http.MethodGet, "/metadata", "/test.Metadata/Get", false, func(ctx context.Context, _ *app.RequestContext) {
		exchange, ok := httpmeta.FromContext(ctx)
		if !ok {
			t.Fatal("missing HTTP metadata")
		}
		observed = exchange.Request()
	})
	request := httptest.NewRequest(http.MethodGet, "https://admin.example/metadata?filter=alice%20smith", nil)
	response := httptest.NewRecorder()
	serveNative(router, response, request)
	if observed == nil || observed.RequestURI != "/metadata?filter=alice%20smith" || observed.Host != "admin.example" || observed.TLS == nil {
		t.Fatalf("native request target metadata = %+v", observed)
	}
}

func TestNativeCORSPreflightAndMethodErrors(t *testing.T) {
	router, err := newAPIRouter(&conf.Bootstrap{
		Server: &conf.Server{
			Rest: &conf.Server_REST{
				Cors: &conf.Server_REST_CORS{
					Origins:          []string{"https://admin.example"},
					Methods:          []string{http.MethodGet},
					Headers:          []string{"Authorization"},
					AllowCredentials: true,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	router.Register(http.MethodGet, "/resource", "/test.Resource/Get", false, func(context.Context, *app.RequestContext) {})
	request := httptest.NewRequest(http.MethodOptions, "/resource", nil)
	request.Header.Set("Origin", "https://admin.example")
	request.Header.Set("Access-Control-Request-Method", http.MethodGet)
	response := httptest.NewRecorder()
	serveNative(router, response, request)
	if response.Code != http.StatusNoContent || response.Header().Get("Access-Control-Allow-Origin") != "https://admin.example" || response.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("preflight = %d %s %v", response.Code, response.Body.String(), response.Header())
	}
	response = httptest.NewRecorder()
	serveNative(router, response, httptest.NewRequest(http.MethodPost, "/resource", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method = %d %s", response.Code, response.Body.String())
	}
}

func TestNativeSwaggerServesEmbeddedUIAndOpenAPI(t *testing.T) {
	router := contractRouter(t)
	registerSwagger(router)
	for _, path := range []string{"/docs/", "/docs/openapi.yaml", "/docs/swagger-ui.css"} {
		response := httptest.NewRecorder()
		serveNative(router, response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || response.Body.Len() == 0 {
			t.Errorf("GET %s = %d %q", path, response.Code, response.Body.String())
		}
	}
}

func TestNativeDownloadRejectsResponseHeaderInjection(t *testing.T) {
	router := contractRouter(t)
	files := &contractFiles{content: []byte("file")}
	RegisterFileTransferServiceHertzServer(router, files)
	response := httptest.NewRecorder()
	serveNative(router, response, httptest.NewRequest(http.MethodGet, "/admin/v1/file/download?fileId=1&disposition=attachment%0D%0AX-Injected:%20yes", nil))
	if response.Code != http.StatusBadRequest || response.Header().Get("X-Injected") != "" {
		t.Fatalf("unsafe disposition = %d %v", response.Code, response.Header())
	}
}
