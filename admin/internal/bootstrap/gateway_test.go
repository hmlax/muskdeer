package bootstrap

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/go-kratos/kratos/v2/transport"
	kgrpc "github.com/go-kratos/kratos/v2/transport/grpc"
	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	"github.com/tx7do/go-crud/viewer"
	"github.com/tx7do/go-utils/trans"
	conf "github.com/tx7do/kratos-bootstrap/api/gen/go/conf/v1"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/emptypb"

	apiV1 "github.com/hmlax/muskdeer/admin/biz/model/admin/v1"
	"github.com/hmlax/muskdeer/deer/management/middleware/auth"
	"github.com/hmlax/muskdeer/deer/management/netutil"
	"github.com/hmlax/muskdeer/deer/management/rpcbridge"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

type observedUserCall struct {
	id, user, tenant uint32
	viewerTenant     uint64
}
type testUsers struct {
	operatorV1.UnimplementedUserServiceServer
	calls    atomic.Int32
	observed chan observedUserCall
}

func (s *testUsers) Get(ctx context.Context, in *operatorV1.GetUserRequest) (*operatorV1.User, error) {
	actor, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	v, ok := viewer.FromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("missing viewer")
	}
	s.calls.Add(1)
	s.observed <- observedUserCall{
		id:           in.GetId(),
		user:         actor.GetUserId(),
		tenant:       actor.GetTenantId(),
		viewerTenant: v.TenantID(),
	}
	return &operatorV1.User{
		Id:       trans.Ptr(in.GetId()),
		Username: trans.Ptr("alice"),
	}, nil
}
func (s *testUsers) List(context.Context, *paginationV1.PagingRequest) (*operatorV1.ListUserResponse, error) {
	return &operatorV1.ListUserResponse{}, nil
}

type testAuthentication struct {
	operatorV1.UnimplementedAuthenticationServiceServer
}

func (s *testAuthentication) RefreshToken(ctx context.Context, _ *operatorV1.LoginRequest) (*operatorV1.LoginResponse, error) {
	if netutil.CookieFromContext(ctx, "refresh_token") != "old-refresh" {
		return nil, operatorV1.ErrorAuthenticationUnauthorized("refresh cookie was lost")
	}
	tr, ok := transport.FromServerContext(ctx)
	if !ok {
		return nil, fmt.Errorf("missing transport")
	}
	tr.ReplyHeader().Add("Set-Cookie", "refresh_token=new-refresh; Path=/admin/v1/refresh-token; HttpOnly; SameSite=Lax")
	tr.ReplyHeader().Add("Set-Cookie", "refresh_exp=9999999999; Path=/; SameSite=Lax")
	return &operatorV1.LoginResponse{AccessToken: "new-access"}, nil
}

type testGateway struct {
	operatorV1.UnimplementedGatewayServiceServer
	access atomic.Pointer[operatorV1.TenantAccessData]
}

func (s *testGateway) GetTenantAccessData(context.Context, *operatorV1.TenantAccessRequest) (*operatorV1.TenantAccessData, error) {
	return s.access.Load(), nil
}
func (*testGateway) ResolveTenantCode(_ context.Context, request *operatorV1.ResolveTenantCodeRequest) (*operatorV1.ResolveTenantCodeResponse, error) {
	if request.GetCode() != "TEST_MERCHANT" {
		return nil, operatorV1.ErrorAdminForbidden("unknown merchant")
	}
	return &operatorV1.ResolveTenantCodeResponse{TenantId: 7}, nil
}
func (*testGateway) GetPolicies(context.Context, *emptypb.Empty) (*operatorV1.PoliciesResponse, error) {
	return &operatorV1.PoliciesResponse{Policies: []*operatorV1.RolePolicy{{
		Role: "tenant:manager",
		Permissions: []*operatorV1.GatewayPermission{{
			Domain: "7",
			Path:   "/admin/v1/users/{id}",
			Method: "GET",
		}},
	}}}, nil
}
func (*testGateway) WriteAudit(context.Context, *operatorV1.AuditRecord) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}
func (*testGateway) ResolvePolicy(context.Context, *operatorV1.ResolvePolicyRequest) (*operatorV1.ResolvePolicyResponse, error) {
	return &operatorV1.ResolvePolicyResponse{}, nil
}
func activeAccess() *operatorV1.TenantAccessData {
	return &operatorV1.TenantAccessData{
		TenantExists: true,
		Status:       trans.Ptr(operatorV1.Tenant_ON.String()),
		ApiExists:    true,
	}
}

func gatewayFixture(t *testing.T) (*Router, *testUsers, *testGateway) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	users := &testUsers{observed: make(chan observedUserCall, 1)}
	support := new(testGateway)
	support.access.Store(activeAccess())
	backend := kgrpc.NewServer(kgrpc.Listener(listener), kgrpc.Middleware(rpcbridge.Server()))
	operatorV1.RegisterUserServiceServer(backend, users)
	operatorV1.RegisterAuthenticationServiceServer(backend, &testAuthentication{})
	operatorV1.RegisterGatewayServiceServer(backend, support)
	done := make(chan error, 1)
	go func() { done <- backend.Start(context.Background()) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = backend.Stop(ctx)
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("server stopped with error: %v", err)
			}
		case <-ctx.Done():
			t.Error("gRPC test server did not stop")
		}
	})
	conn, err := kgrpc.DialInsecure(t.Context(), kgrpc.WithEndpoint(listener.Addr().String()), kgrpc.WithTimeout(3*time.Second), kgrpc.WithMiddleware(rpcbridge.Client()), kgrpc.WithUnaryInterceptor(rpcbridge.ReplyHeaders()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := operatorV1.NewGatewayServiceClient(conn)
	actor := &operatorV1.UserTokenPayload{
		UserId:   42,
		TenantId: trans.Ptr(uint32(7)),
		Roles:    []string{"tenant:manager"},
	}
	checker := auth.NewAccessTokenCheckerFromFuncs(func(_ context.Context, token string, _ bool) (bool, *operatorV1.UserTokenPayload) {
		return token == "allowed", actor
	}, nil)
	ctx := bootstrap.NewContextWithParam(t.Context(), &conf.AppInfo{}, &conf.Bootstrap{}, bLogger.NopLogger())
	middlewares, err := NewRestMiddleware(ctx, checker, newTenantChecker(client), client)
	if err != nil {
		t.Fatal(err)
	}
	router, err := newAPIRouter(ctx.GetConfig(), middlewares...)
	if err != nil {
		t.Fatal(err)
	}
	registerServices(router, conn)
	return router, users, support
}
func TestGatewayAuthorizesBeforeForwardingAndRestoresActor(t *testing.T) {
	router, users, support := gatewayFixture(t)
	for _, test := range []struct {
		name, token string
		access      *operatorV1.TenantAccessData
		status      int
	}{
		{
			name:   "missing token",
			access: activeAccess(),
			status: 401,
		},
		{
			name:   "invalid token",
			token:  "invalid",
			access: activeAccess(),
			status: 401,
		},
		{
			name:  "tenant suspended",
			token: "allowed",
			access: &operatorV1.TenantAccessData{
				TenantExists: true,
				Status:       trans.Ptr("OFF"),
			},
			status: 403,
		},
		{
			name:  "tenant expired",
			token: "allowed",
			access: &operatorV1.TenantAccessData{
				TenantExists: true,
				Status:       trans.Ptr(operatorV1.Tenant_ON.String()),
				ExpiresAt:    trans.Ptr(time.Now().Add(-time.Minute).Unix()),
				ApiExists:    true,
			},
			status: 403,
		},
		{
			name:   "permission denied",
			token:  "allowed",
			access: activeAccess(),
			status: 403,
		},
		{
			name:   "authorized",
			token:  "allowed",
			access: activeAccess(),
			status: 200,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			support.access.Store(test.access)
			method := http.MethodGet
			if test.name == "permission denied" {
				method = http.MethodDelete
			}
			req := httptest.NewRequest(method, "/admin/v1/users/9", nil)
			if test.token != "" {
				req.Header.Set("Authorization", "Bearer "+test.token)
			}
			// A public caller must never be able to choose a forwarded principal.
			req.Header.Set("X-Gateway-Actor", "forged")
			req.Header.Set("X-Gateway-Portal-Tenant", "0")
			req.Header.Set("X-Tenant-Code", "TEST_MERCHANT")
			before := users.calls.Load()
			w := httptest.NewRecorder()
			serveNative(router, w, req)
			if w.Code != test.status {
				t.Fatalf("gateway %s = %d %s, want %d", test.name, w.Code, w.Body.String(), test.status)
			}
			if test.status != 200 {
				if users.calls.Load() != before {
					t.Fatal("rejected request reached operator business RPC")
				}
				return
			}
			var out apiV1.User
			if err := protojson.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if out.GetId() != 9 {
				t.Fatalf("Get user id = %d, want 9", out.GetId())
			}
			call := <-users.observed
			if call.id != 9 || call.user != 42 || call.tenant != 7 || call.viewerTenant != 7 {
				t.Fatalf("forwarded user context = %+v, want id=9 uid=42 tenant=7 viewer=7", call)
			}
		})
	}
}
func TestGatewayRefreshCookiesSurviveGRPC(t *testing.T) {
	router, _, _ := gatewayFixture(t)
	request := httptest.NewRequest(http.MethodPost, "/admin/v1/refresh-token", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{
		Name:  "refresh_token",
		Value: "old-refresh",
	})
	response := httptest.NewRecorder()
	serveNative(router, response, request)
	if response.Code != 200 {
		t.Fatalf("refresh = %d %s, want 200", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 2 || cookies[0].Value != "new-refresh" || !cookies[0].HttpOnly || cookies[1].Name != "refresh_exp" {
		t.Fatalf("refresh cookies = %+v, want two intact cookies", cookies)
	}
}
func TestHertzOwnsHTTPListener(t *testing.T) {
	server, err := NewHTTPServer("127.0.0.1:0", "", &conf.Bootstrap{})
	if err != nil {
		t.Fatal(err)
	}
	server.router.POST("/admin/v1/test", func(_ context.Context, c *app.RequestContext) {
		c.Response.Header.Set("X-Adapter", "protobuf")
		c.Data(http.StatusOK, "application/json", c.Request.Body())
	})
	done := make(chan error, 1)
	go func() { done <- server.Start(context.Background()) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Stop(ctx)
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("server stopped with error: %v", err)
			}
		case <-ctx.Done():
			t.Error("Hertz test server did not stop")
		}
	})
	endpoint, err := server.Endpoint()
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	response, err := client.Post(endpoint.String()+"/admin/v1/test", "application/json", bytes.NewBufferString(`{"data":{"name":"test"}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || response.Header.Get("X-Adapter") != "protobuf" || string(body) != `{"data":{"name":"test"}}` {
		t.Fatalf("Hertz adapter = %d %s, want unchanged JSON body", response.StatusCode, body)
	}
}
