package adminhttp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type auditHTTPClient struct {
	strategyHTTPClient
	last    *strategyv1.ListAuditsRequest
	wrongID bool
}

func (c *auditHTTPClient) ListAudits(ctx context.Context, request *strategyv1.ListAuditsRequest, _ ...grpc.CallOption) (*strategyv1.ListAuditsReply, error) {
	c.record(ctx, request.Session)
	c.last = proto.Clone(request).(*strategyv1.ListAuditsRequest)
	return &strategyv1.ListAuditsReply{
		Audits:      []*strategyv1.AuditEntry{auditHTTPEntry(9007199254740993)},
		NextAfterId: 9007199254740993,
	}, c.failure
}

func (c *auditHTTPClient) GetAudit(ctx context.Context, request *strategyv1.GetAuditRequest, _ ...grpc.CallOption) (*strategyv1.GetAuditReply, error) {
	c.record(ctx, request.Session)
	id := request.AuditId
	if c.wrongID {
		id++
	}
	return &strategyv1.GetAuditReply{Audit: auditHTTPEntry(id)}, c.failure
}

func auditHTTPEntry(id int64) *strategyv1.AuditEntry {
	return &strategyv1.AuditEntry{
		Id:           id,
		Actor:        "history-actor",
		ActorRole:    string(domain.AdminRoleOperator),
		Action:       domain.AdminStrategyCreate,
		Resource:     domain.AdminAuditResourceStrategy + ":9007199254740993",
		Reason:       "历史原因 <script> remains text",
		AfterVersion: proto.Int64(9007199254740994),
		CreatedAtMs:  1793200000000,
	}
}

func auditHTTPFixture(t *testing.T, client strategyv1.StrategyAuditClient) *server.Hertz {
	t.Helper()
	routes, err := New(&authFixture{}, testOrigin, true)
	if err != nil {
		t.Fatal(err)
	}
	routes.audits = client
	hertz := server.New()
	routes.Register(hertz)
	return hertz
}

func TestAuditHTTPKeepsCanonicalFiltersOptionalVersionsAndOwnedCookie(t *testing.T) {
	client := &auditHTTPClient{}
	hertz := auditHTTPFixture(t, client)
	cookie := ut.Header{
		Key:   "Cookie",
		Value: secureSessionCookie + "=" + testSecret(1),
	}
	reply := perform(hertz, "GET", "/api/v1/audits?after_id=9007199254740992&limit=1&actor=history-actor&action=strategy.create&resource=strategy_instance%3A9007199254740993", "", cookie)
	if reply.Code != 200 || client.last == nil || client.last.AfterId != 9007199254740992 || client.last.Limit != 1 || client.last.Actor != "history-actor" || client.last.Action != domain.AdminStrategyCreate || client.last.Resource != domain.AdminAuditResourceStrategy+":9007199254740993" || client.token != testSecret(1) || !client.bounded {
		t.Fatal("audit HTTP changed exact filters, authority or native budget", reply.Code)
	}
	var page strategyv1.ListAuditsReply
	if err := protojson.Unmarshal(reply.Body.Bytes(), &page); err != nil || len(page.Audits) != 1 || page.Audits[0].Id != 9007199254740993 || page.Audits[0].BeforeVersion != nil || page.Audits[0].GetAfterVersion() != 9007199254740994 {
		t.Fatal("audit HTTP lost int64 precision or optional revision", err)
	}
	var wire struct {
		Audits []struct {
			ID            string  `json:"id"`
			BeforeVersion *string `json:"before_version"`
			AfterVersion  string  `json:"after_version"`
		} `json:"audits"`
	}
	if err := json.Unmarshal(reply.Body.Bytes(), &wire); err != nil || len(wire.Audits) != 1 || wire.Audits[0].ID != "9007199254740993" || wire.Audits[0].BeforeVersion != nil || wire.Audits[0].AfterVersion != "9007199254740994" {
		t.Fatal("audit HTTP encoded revisions as numbers or invented optional presence", err)
	}
	if strings.Contains(reply.Body.String(), testSecret(1)) || string(reply.Header().Peek("Cache-Control")) != "no-store" {
		t.Fatal("audit HTTP invented presence, exposed a cookie or allowed caching")
	}
	detail := perform(hertz, "GET", "/api/v1/audits/9007199254740993", "", cookie)
	var value strategyv1.GetAuditReply
	if err := protojson.Unmarshal(detail.Body.Bytes(), &value); detail.Code != 200 || err != nil || value.Audit.Id != page.Audits[0].Id || value.Audit.Reason != page.Audits[0].Reason {
		t.Fatal("audit detail changed historical identity or reason", err)
	}
	client.wrongID = true
	if mismatch := perform(hertz, "GET", "/api/v1/audits/9007199254740993", "", cookie); mismatch.Code != 503 {
		t.Fatal("audit HTTP exposed a mismatched detail", mismatch.Code)
	}
}

func TestAuditHTTPRejectsAmbiguousInputAndMissingAuthorityBeforeRPC(t *testing.T) {
	client := &auditHTTPClient{}
	hertz := auditHTTPFixture(t, client)
	cookie := ut.Header{
		Key:   "Cookie",
		Value: secureSessionCookie + "=" + testSecret(1),
	}
	for _, path := range []string{
		"/api/v1/audits?actor=a&actor=b",
		"/api/v1/audits?resource=one&resource=two",
		"/api/v1/audits?action=a&action=b",
		"/api/v1/audits?limit=1&limit=2",
		"/api/v1/audits?limit=0",
		"/api/v1/audits?limit=101",
		"/api/v1/audits?after_id=01",
		"/api/v1/audits?after_id=-1",
		"/api/v1/audits?after_id=9223372036854775808",
		"/api/v1/audits?session=forged",
		"/api/v1/audits?actor=%20space",
		"/api/v1/audits?resource=%00",
		"/api/v1/audits?resource=%FF",
		"/api/v1/audits?resource=%ZZ",
		"/api/v1/audits?resource=" + strings.Repeat("x", 129),
		"/api/v1/audits?actor=" + strings.Repeat("x", 1025),
		"/api/v1/audits/0",
		"/api/v1/audits/01",
		"/api/v1/audits/9223372036854775808",
		"/api/v1/audits/1?actor=forged",
	} {
		if reply := perform(hertz, "GET", path, "", cookie); reply.Code != 400 {
			t.Fatal("ambiguous audit input reached RPC", path, reply.Code)
		}
	}
	for _, path := range []string{"/api/v1/audits", "/api/v1/audits/1"} {
		if reply := perform(hertz, "GET", path, "", ut.Header{Key: "Cookie", Value: cookie.Value + "; " + cookie.Value}); reply.Code != 401 {
			t.Fatal("audit endpoint accepted duplicate session cookies", reply.Code)
		}
		if reply := perform(hertz, "GET", path, ""); reply.Code != 401 {
			t.Fatal("audit endpoint accepted missing authority", reply.Code)
		}
		if reply := perform(hertz, "GET", path, "{}", cookie); reply.Code != 400 {
			t.Fatal("audit endpoint accepted a body", reply.Code)
		}
		if reply := perform(hertz, "POST", path, "", cookie); reply.Code != 405 {
			t.Fatal("audit endpoint exposed a write", reply.Code)
		}
	}
	if client.calls != 0 {
		t.Fatal("invalid audit boundary reached RPC")
	}
}

func TestAuditHTTPKeepsFixedFailuresAndDisabledRoutes(t *testing.T) {
	client := &auditHTTPClient{}
	hertz := auditHTTPFixture(t, client)
	cookie := ut.Header{
		Key:   "Cookie",
		Value: secureSessionCookie + "=" + testSecret(1),
	}
	for _, failure := range []struct {
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
		client.failure = status.Error(failure.code, "private persistence detail")
		if reply := perform(hertz, "GET", "/api/v1/audits/1", "", cookie); reply.Code != failure.status || strings.Contains(reply.Body.String(), "private persistence") {
			t.Fatal("audit HTTP changed the fixed failure contract", reply.Code)
		}
	}
	disabled := auditHTTPFixture(t, nil)
	if reply := perform(disabled, "GET", "/api/v1/audits", "", cookie); reply.Code != 404 {
		t.Fatal("audit route ignored explicit query opt-in", reply.Code)
	}
}
