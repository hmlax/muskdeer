package adminhttp

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

const controlHTTPBody = `{"action":"` + string(domain.AdminControlStop) + `","expected_revision":"9007199254740993","reason":"explicit operator reason"}`

func TestControlHTTPPreservesProofLongRevisionsAndAsyncStatus(t *testing.T) {
	client := &controlHTTPClient{}
	hertz := controlHTTPFixture(t, client)
	result := perform(hertz, "POST", "/api/v1/strategies/9007199254740993/control", controlHTTPBody, controlHTTPHeaders()...)
	var receipt struct {
		ID               string  `json:"id"`
		ExpectedRevision string  `json:"expected_revision"`
		State            string  `json:"state"`
		CompletedAtMS    *string `json:"completed_at_ms"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &receipt); err != nil || result.Code != 202 || receipt.ID != "9007199254740993" || receipt.ExpectedRevision != "9007199254740993" || receipt.State != string(domain.AdminOperationAccepted) || receipt.CompletedAtMS != nil {
		t.Fatalf("control acceptance lost exact identity/status: status=%d calls=%d id=%s state=%s err=%v", result.Code, client.calls, receipt.ID, receipt.State, err)
	}
	if string(result.Header().Peek("Location")) != "/api/v1/operations/9007199254740993" || string(result.Header().Peek("Cache-Control")) != "no-store" || client.token != testSecret(1) || client.csrf != testSecret(2) || !client.bounded || client.request.IdempotencyKey != "http-control-original-key" || client.request.StrategyId != 9007199254740993 || strings.Contains(result.Body.String(), testSecret(1)) || strings.Contains(result.Body.String(), testSecret(2)) {
		t.Fatal("HTTP lost its cookie/header proof, native budget or private-secret boundary")
	}
	client.terminal = true
	if result := perform(hertz, "POST", "/api/v1/strategies/9007199254740993/control", controlHTTPBody, controlHTTPHeaders()...); result.Code != 200 {
		t.Fatal("completed replay did not retain its final result")
	}
	result = perform(hertz, "GET", "/api/v1/operations/9007199254740993", "", controlHTTPHeaders()[1])
	var proof struct {
		Observation struct {
			Revision string `json:"strategy_revision"`
		} `json:"observation"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &proof); err != nil || result.Code != 200 || proof.Observation.Revision != "9007199254740994" || client.token != testSecret(1) {
		t.Fatal("operation read lost persistent proof or required write CSRF")
	}
}

func TestControlHTTPRejectsAmbiguityAndCrossOriginBeforeRPC(t *testing.T) {
	client := &controlHTTPClient{}
	hertz := controlHTTPFixture(t, client)
	path := "/api/v1/strategies/1/control"
	for _, body := range []string{
		`{}`, `null`,
		`{"action":"` + string(domain.AdminControlStop) + `","expected_revision":2,"reason":"stop"}`,
		`{"action":"` + string(domain.AdminControlStop) + `","expected_revision":"02","reason":"stop"}`,
		`{"action":"` + string(domain.AdminControlStop) + `","expected_revision":"9223372036854775807","reason":"stop"}`,
		`{"action":"` + string(domain.AdminControlStop) + `","action":"` + string(domain.AdminControlStart) + `","expected_revision":"2","reason":"stop"}`,
		`{"action":"` + string(domain.AdminControlStop) + `","expected_revision":"2","reason":null}`,
		`{"action":"` + string(domain.AdminControlStop) + `","expected_revision":"2","reason":"stop","session":"forged"}`,
		`{"action":"strategy.close_position","expected_revision":"2","reason":"stop"}`,
		controlHTTPBody + `{}`,
	} {
		if reply := perform(hertz, "POST", path, body, controlHTTPHeaders()...); reply.Code != 400 {
			t.Fatalf("ambiguous control body reached RPC: status=%d calls=%d", reply.Code, client.calls)
		}
	}
	for _, path := range []string{"/api/v1/strategies/01/control", "/api/v1/strategies/0/control", "/api/v1/strategies/1/control?session=forged"} {
		if reply := perform(hertz, "POST", path, controlHTTPBody, controlHTTPHeaders()...); reply.Code != 400 {
			t.Fatal("ambiguous control path reached RPC")
		}
	}
	for index, expected := range []int{403, 401, 403, 400} {
		headers := controlHTTPHeaders()
		headers = append(headers[:index], headers[index+1:]...)
		if reply := perform(hertz, "POST", path, controlHTTPBody, headers...); reply.Code != expected {
			t.Fatalf("missing independent proof accepted: field=%d status=%d", index, reply.Code)
		}
		headers = controlHTTPHeaders()
		headers = append(slices.Clone(headers), headers[index])
		if reply := perform(hertz, "POST", path, controlHTTPBody, headers...); reply.Code != expected {
			t.Fatalf("duplicated proof accepted: field=%d status=%d", index, reply.Code)
		}
	}
	if client.calls != 0 {
		t.Fatal("invalid control caused an RPC")
	}
}

func TestControlHTTPMapsConflictAndPrivateFailuresAndKeepsOptIn(t *testing.T) {
	for _, test := range []struct {
		code   codes.Code
		status int
	}{
		{
			code:   codes.Aborted,
			status: 409,
		},
		{
			code:   codes.PermissionDenied,
			status: 403,
		},
		{
			code:   codes.Unauthenticated,
			status: 401,
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
		client := &controlHTTPClient{failure: status.Error(test.code, "private persistence cause")}
		hertz := controlHTTPFixture(t, client)
		result := perform(hertz, "POST", "/api/v1/strategies/1/control", controlHTTPBody, controlHTTPHeaders()...)
		if result.Code != test.status || strings.Contains(result.Body.String(), "private") || len(result.Header().Peek("Location")) != 0 {
			t.Fatalf("control public status or failure redaction changed: code=%d expected=%d calls=%d", result.Code, test.status, client.calls)
		}
	}
	hertz := strategyHTTPFixture(t, &strategyHTTPClient{})
	if result := perform(hertz, "POST", "/api/v1/strategies/1/control", controlHTTPBody, controlHTTPHeaders()...); result.Code != 404 {
		t.Fatal("read-only management exposed controls")
	}
	if result := perform(hertz, "GET", "/api/v1/operations/1", "", controlHTTPHeaders()[1]); result.Code != 404 {
		t.Fatal("read-only management exposed operation results")
	}
}

func controlHTTPHeaders() []ut.Header {
	return []ut.Header{
		{
			Key:   "Origin",
			Value: testOrigin,
		},
		{
			Key:   "Cookie",
			Value: secureSessionCookie + "=" + testSecret(1),
		},
		{
			Key:   CSRFHeader,
			Value: testSecret(2),
		},
		{
			Key:   IdempotencyHeader,
			Value: "http-control-original-key",
		},
	}
}

func controlHTTPFixture(t *testing.T, client strategyv1.StrategyControlClient) *server.Hertz {
	t.Helper()
	routes, err := New(&authFixture{}, testOrigin, true)
	if err != nil {
		t.Fatal(err)
	}
	routes.strategies = &strategyHTTPClient{}
	routes.controls = client
	hertz := server.New()
	routes.Register(hertz)
	return hertz
}

type controlHTTPClient struct {
	calls    int
	token    string
	csrf     string
	request  *strategyv1.ControlStrategyRequest
	bounded  bool
	terminal bool
	failure  error
}

func (c *controlHTTPClient) ControlStrategy(ctx context.Context, request *strategyv1.ControlStrategyRequest, _ ...grpc.CallOption) (*strategyv1.StrategyOperation, error) {
	c.calls++
	c.token, c.csrf = request.Session.Token, request.CsrfToken
	c.request = proto.CloneOf(request)
	deadline, ok := ctx.Deadline()
	c.bounded = ok && time.Until(deadline) <= strategyReadTimeout && ctx.Err() == nil
	state := domain.AdminOperationAccepted
	if c.terminal {
		state = domain.AdminOperationSucceeded
	}
	return &strategyv1.StrategyOperation{
		Id:               9007199254740993,
		StrategyId:       request.StrategyId,
		ExpectedRevision: request.ExpectedRevision,
		State:            string(state),
	}, c.failure
}

func (c *controlHTTPClient) GetOperation(_ context.Context, request *strategyv1.GetOperationRequest, _ ...grpc.CallOption) (*strategyv1.StrategyOperation, error) {
	c.calls++
	c.token = request.Session.Token
	return &strategyv1.StrategyOperation{
		Id:            request.OperationId,
		State:         string(domain.AdminOperationSucceeded),
		CompletedAtMs: proto.Int64(1700000000000),
		Observation: &strategyv1.OperationObservation{
			StrategyRevision: 9007199254740994,
			State:            string(domain.StrategyStateStopped),
		},
	}, c.failure
}
