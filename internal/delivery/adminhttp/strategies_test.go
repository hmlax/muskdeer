package adminhttp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
)

func TestStrategyHTTPUsesCookieAndTypedDecimalAndLongIdentityWire(t *testing.T) {
	client := &strategyHTTPClient{}
	hertz := strategyHTTPFixture(t, client)
	cookie := ut.Header{
		Key:   "Cookie",
		Value: secureSessionCookie + "=" + testSecret(1),
	}
	for _, path := range []string{"/api/v1/strategies?limit=1&after_id=9007199254740993", "/api/v1/strategies/9007199254740993", "/api/v1/strategies/9007199254740993/versions"} {
		reply := perform(hertz, "GET", path, "", cookie)
		if reply.Code != 200 || client.token != testSecret(1) || !client.bounded || strings.Contains(reply.Body.String(), testSecret(1)) {
			t.Fatal("management read lost cookie/budget or leaked credentials")
		}
		if strings.Contains(path, "?limit") {
			var page struct {
				NextAfterID string `json:"next_after_id"`
			}
			if err := json.Unmarshal(reply.Body.Bytes(), &page); err != nil || page.NextAfterID != "9007199254740993" {
				t.Fatal("HTTP rounded a long integer identity", err)
			}
		}
		if strings.HasSuffix(path, "/9007199254740993") {
			var detail strategyv1.GetStrategyReply
			if err := protojson.Unmarshal(reply.Body.Bytes(), &detail); err != nil || detail.GetRequestedConfig().GetParameters().GetGrid().GetLowerPrice() != "0.123456789012345678" {
				t.Fatal("HTTP changed decimal precision or JSON type", err)
			}
		}
		if string(reply.Header().Peek("Cache-Control")) != "no-store" {
			t.Fatal("management data became cacheable")
		}
	}
	if client.calls != 3 {
		t.Fatal("HTTP used a local business-data bypass")
	}
}

func TestStrategyHTTPRejectsAmbiguityAndMissingCookiesBeforeRPC(t *testing.T) {
	client := &strategyHTTPClient{}
	hertz := strategyHTTPFixture(t, client)
	cookie := ut.Header{
		Key:   "Cookie",
		Value: secureSessionCookie + "=" + testSecret(1),
	}
	for _, path := range []string{
		"/api/v1/strategies?limit=1&limit=2",
		"/api/v1/strategies?limit=101",
		"/api/v1/strategies?limit=0",
		"/api/v1/strategies?after_id=-1",
		"/api/v1/strategies?after_id=9223372036854775808",
		"/api/v1/strategies?session=forged",
		"/api/v1/strategies/01",
		"/api/v1/strategies/1?session=forged",
	} {
		if reply := perform(hertz, "GET", path, "", cookie); reply.Code != 400 {
			t.Fatal("ambiguous management query reached RPC")
		}
	}
	if reply := perform(hertz, "GET", "/api/v1/strategies", ""); reply.Code != 401 {
		t.Fatal("missing cookie reached business RPC")
	}
	if reply := perform(hertz, "POST", "/api/v1/strategies", "{}", cookie); reply.Code != 405 {
		t.Fatal("query endpoint exposed a management write")
	}
	if client.calls != 0 {
		t.Fatal("invalid request caused a business read")
	}
}

func TestStrategyHTTPMapsBusinessFailuresWithoutPrivateDetails(t *testing.T) {
	for _, test := range []struct {
		code   codes.Code
		status int
	}{
		{
			code:   codes.Unauthenticated,
			status: 401,
		},
		{
			code:   codes.PermissionDenied,
			status: 403,
		},
		{
			code:   codes.NotFound,
			status: 404,
		},
		{
			code:   codes.Unavailable,
			status: 503,
		},
	} {
		client := &strategyHTTPClient{failure: status.Error(test.code, "private provider detail")}
		reply := perform(strategyHTTPFixture(t, client), "GET", "/api/v1/strategies", "", ut.Header{
			Key:   "Cookie",
			Value: secureSessionCookie + "=" + testSecret(1),
		})
		if reply.Code != test.status || strings.Contains(reply.Body.String(), "private provider") {
			t.Fatal("business status or error redaction changed")
		}
	}
}

func strategyHTTPFixture(t *testing.T, client strategyv1.StrategyManagementClient) *server.Hertz {
	t.Helper()
	routes, err := New(&authFixture{}, testOrigin, true)
	if err != nil {
		t.Fatal(err)
	}
	routes.strategies = client
	hertz := server.New()
	routes.Register(hertz)
	return hertz
}

type strategyHTTPClient struct {
	strategyv1.StrategyManagementClient
	calls   int
	token   string
	bounded bool
	failure error
}

func (c *strategyHTTPClient) record(ctx context.Context, session *strategyv1.AdminSession) {
	c.calls++
	c.token = session.GetToken()
	deadline, ok := ctx.Deadline()
	c.bounded = ok && time.Until(deadline) <= strategyReadTimeout && ctx.Err() == nil
}

func (c *strategyHTTPClient) ListStrategies(ctx context.Context, request *strategyv1.ListStrategiesRequest, _ ...grpc.CallOption) (*strategyv1.ListStrategiesReply, error) {
	c.record(ctx, request.Session)
	return &strategyv1.ListStrategiesReply{NextAfterId: request.AfterId}, c.failure
}

func (c *strategyHTTPClient) GetStrategy(ctx context.Context, request *strategyv1.GetStrategyRequest, _ ...grpc.CallOption) (*strategyv1.GetStrategyReply, error) {
	c.record(ctx, request.Session)
	return &strategyv1.GetStrategyReply{RequestedConfig: &strategyv1.ConfigVersion{Parameters: &commonv1.StrategyParameters{
		Template: &commonv1.StrategyParameters_Grid{Grid: &commonv1.GridParameters{LowerPrice: "0.123456789012345678"}},
	}}}, c.failure
}

func (c *strategyHTTPClient) ListConfigVersions(ctx context.Context, request *strategyv1.ListConfigVersionsRequest, _ ...grpc.CallOption) (*strategyv1.ListConfigVersionsReply, error) {
	c.record(ctx, request.Session)
	return &strategyv1.ListConfigVersionsReply{}, c.failure
}
