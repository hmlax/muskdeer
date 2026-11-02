package adminhttp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

const emergencyHTTPPath = "/api/v1/strategies/9007199254740994/emergency-cancellations"
const emergencyObservationHTTPPath = emergencyHTTPPath + "/9007199254740993"
const emergencyHTTPBody = `{"scope":"strategy","expected_risk_revision":"9007199254740997","reason":"  confirm this strategy scope  "}`

type emergencyHTTPClient struct {
	strategyHTTPClient
	request *strategyv1.RequestEmergencyCancellationRequest
	mutate  func(*strategyv1.EmergencyCancellationAcceptance)
}

func TestEmergencyHTTPRejectsMismatchedSuccessAndReturnsFixedFailures(t *testing.T) {
	client := &emergencyHTTPClient{}
	hertz := emergencyHTTPFixture(t, client, client)
	for _, change := range []func(*strategyv1.EmergencyCancellationAcceptance){
		func(r *strategyv1.EmergencyCancellationAcceptance) { r.StrategyId++ },
		func(r *strategyv1.EmergencyCancellationAcceptance) { r.RiskRevision++ },
		func(r *strategyv1.EmergencyCancellationAcceptance) { r.Reason = "different confirmation" },
		func(r *strategyv1.EmergencyCancellationAcceptance) { r.Scope = "account" },
	} {
		client.mutate = change
		if reply := perform(hertz, "POST", emergencyHTTPPath, emergencyHTTPBody, controlHTTPHeaders()...); reply.Code != 503 || reply.Header().Get("Location") != "" {
			t.Fatal("mismatched acceptance escaped HTTP or advertised a result location", reply.Code)
		}
	}
	client.mutate = func(r *strategyv1.EmergencyCancellationAcceptance) { r.Id++ }
	if reply := perform(hertz, "GET", emergencyObservationHTTPPath, "", controlHTTPHeaders()...); reply.Code != 503 {
		t.Fatal("emergency observation accepted mismatched identity", reply.Code)
	}
	client.mutate = nil
	for _, test := range []struct {
		code codes.Code
		want int
	}{
		{
			code: codes.Aborted,
			want: 409,
		},
		{
			code: codes.PermissionDenied,
			want: 403,
		},
		{
			code: codes.Unauthenticated,
			want: 401,
		},
		{
			code: codes.Unavailable,
			want: 503,
		},
	} {
		client.failure = status.Error(test.code, "private-emergency-server-address")
		if reply := perform(hertz, "POST", emergencyHTTPPath, emergencyHTTPBody, controlHTTPHeaders()...); reply.Code != test.want || strings.Contains(reply.Body.String(), "private-emergency") {
			t.Fatal("emergency HTTP lost fixed failure classification", reply.Code)
		}
	}
}

func (c *emergencyHTTPClient) acceptance() *strategyv1.EmergencyCancellationAcceptance {
	value := &strategyv1.EmergencyCancellationAcceptance{
		Id:                   9007199254740993,
		ActorUserId:          1,
		StrategyId:           9007199254740994,
		Scope:                string(domain.AdminEmergencyCancelScopeStrategy),
		ExpectedRiskRevision: 9007199254740997,
		RiskRevision:         9007199254740998,
		Reason:               "confirm this strategy scope",
		CreatedAtMs:          1700000000000,
	}
	if c.mutate != nil {
		c.mutate(value)
	}
	return value
}

func (c *emergencyHTTPClient) RequestEmergencyCancellation(ctx context.Context, request *strategyv1.RequestEmergencyCancellationRequest, _ ...grpc.CallOption) (*strategyv1.EmergencyCancellationAcceptance, error) {
	c.record(ctx, request.Session)
	c.request = request
	return c.acceptance(), c.failure
}

func (c *emergencyHTTPClient) GetEmergencyCancellation(ctx context.Context, request *strategyv1.GetEmergencyCancellationRequest, _ ...grpc.CallOption) (*strategyv1.EmergencyCancellationObservation, error) {
	c.record(ctx, request.Session)
	return &strategyv1.EmergencyCancellationObservation{
		Record: &strategyv1.EmergencyCancellationRecord{
			Acceptance: c.acceptance(),
			State:      string(domain.AdminEmergencyCancelAccepted),
			Revision:   1,
		},
		Progress:     &strategyv1.EmergencyCancellationProgress{},
		ObservedAtMs: 1700000000001,
	}, c.failure
}

func (c *emergencyHTTPClient) ListEmergencyCancellations(ctx context.Context, request *strategyv1.ListEmergencyCancellationsRequest, _ ...grpc.CallOption) (*strategyv1.ListEmergencyCancellationsReply, error) {
	c.record(ctx, request.Session)
	return &strategyv1.ListEmergencyCancellationsReply{
		Items: []*strategyv1.EmergencyCancellationRecord{
			{
				Acceptance: c.acceptance(),
				State:      string(domain.AdminEmergencyCancelAccepted),
				Revision:   1,
			},
		},
		ObservedAtMs: 1700000000001,
	}, c.failure
}

func emergencyHTTPFixture(t *testing.T, reader strategyv1.StrategyEmergencyCancellationQueryClient, writer strategyv1.StrategyEmergencyCancellationControlClient) *server.Hertz {
	t.Helper()
	routes, err := New(&authFixture{}, testOrigin, true)
	if err != nil {
		t.Fatal(err)
	}
	routes.emergencyQueries, routes.emergencyControls = reader, writer
	hertz := server.New()
	routes.Register(hertz)
	return hertz
}

func TestEmergencyHTTPAcceptsExactScopeAndSeparatesObservationAndHistory(t *testing.T) {
	client := &emergencyHTTPClient{}
	hertz := emergencyHTTPFixture(t, client, client)
	reply := perform(hertz, "POST", emergencyHTTPPath, emergencyHTTPBody, controlHTTPHeaders()...)
	var accepted struct {
		ID       string `json:"id"`
		Strategy string `json:"strategy_id"`
		Scope    string `json:"scope"`
		Expected string `json:"expected_risk_revision"`
		Revision string `json:"risk_revision"`
	}
	if err := json.Unmarshal(reply.Body.Bytes(), &accepted); err != nil || reply.Code != 202 || accepted.ID != "9007199254740993" || accepted.Strategy != "9007199254740994" ||
		accepted.Scope != string(domain.AdminEmergencyCancelScopeStrategy) || accepted.Expected != "9007199254740997" || accepted.Revision != "9007199254740998" {
		t.Fatal("emergency HTTP lost string confirmation or immutable acceptance", err, reply.Code)
	}
	if string(reply.Header().Peek("Location")) != emergencyObservationHTTPPath || client.request.Input.Reason != "confirm this strategy scope" || client.request.CsrfToken != testSecret(2) ||
		client.request.IdempotencyKey != "http-control-original-key" || !client.bounded || client.token != testSecret(1) {
		t.Fatal("emergency HTTP lost scope, normalization or owned caller context")
	}
	if strings.Contains(reply.Body.String(), `"state"`) || strings.Contains(reply.Body.String(), testSecret(1)) || string(reply.Header().Peek("Cache-Control")) != "no-store" {
		t.Fatal("acceptance invented task completion or exposed a session")
	}
	for _, path := range []string{emergencyObservationHTTPPath, emergencyHTTPPath + "?limit=1"} {
		if result := perform(hertz, "GET", path, "", controlHTTPHeaders()...); result.Code != 200 || !strings.Contains(result.Body.String(), `"ACCEPTED"`) || !strings.Contains(result.Body.String(), `"9007199254740993"`) {
			t.Fatal("HTTP query lost its separate stored result", result.Code)
		}
	}
}

func TestEmergencyHTTPRejectsAmbiguousScopeAndIndependentProofBeforeRPC(t *testing.T) {
	client := &emergencyHTTPClient{}
	hertz := emergencyHTTPFixture(t, client, client)
	for _, body := range []string{
		`{}`, `null`, emergencyHTTPBody + `{}`,
		strings.Replace(emergencyHTTPBody, `"strategy"`, `"account"`, 1),
		strings.Replace(emergencyHTTPBody, `"9007199254740997"`, `9007199254740997`, 1),
		strings.Replace(emergencyHTTPBody, `"9007199254740997"`, `"01"`, 1),
		strings.Replace(emergencyHTTPBody, `"9007199254740997"`, `"9223372036854775807"`, 1),
		strings.Replace(emergencyHTTPBody, `"scope":`, `"scope":"strategy","scope":`, 1),
		strings.Replace(emergencyHTTPBody, `"reason":`, `"cancel_all":true,"reason":`, 1),
		strings.Replace(emergencyHTTPBody, `"  confirm this strategy scope  "`, `null`, 1),
		strings.Repeat(" ", maximumControlBytes+1),
	} {
		if reply := perform(hertz, "POST", emergencyHTTPPath, body, controlHTTPHeaders()...); reply.Code != 400 {
			t.Fatal("ambiguous emergency scope reached RPC", reply.Code)
		}
	}
	for index, expected := range []int{403, 401, 403, 400} {
		headers := controlHTTPHeaders()
		headers = append(headers[:index], headers[index+1:]...)
		if reply := perform(hertz, "POST", emergencyHTTPPath, emergencyHTTPBody, headers...); reply.Code != expected {
			t.Fatal("emergency request lost independent proof", index, reply.Code)
		}
	}
	for _, path := range []string{emergencyObservationHTTPPath + "?x=1", strings.Replace(emergencyObservationHTTPPath, "9007199254740993", "01", 1), emergencyHTTPPath + "?limit=0", emergencyHTTPPath + "?limit=1&limit=2"} {
		if reply := perform(hertz, "GET", path, "", controlHTTPHeaders()...); reply.Code != 400 {
			t.Fatal("ambiguous emergency query reached RPC", reply.Code)
		}
	}
	if client.calls != 0 {
		t.Fatal("rejected emergency input reached business RPC")
	}
	if reply := perform(emergencyHTTPFixture(t, client, nil), "POST", emergencyHTTPPath, emergencyHTTPBody, controlHTTPHeaders()...); reply.Code != 405 {
		t.Fatal("query-only route exposed emergency writes", reply.Code)
	}
	if reply := perform(emergencyHTTPFixture(t, nil, nil), "GET", emergencyObservationHTTPPath, "", controlHTTPHeaders()...); reply.Code != 404 {
		t.Fatal("disabled query route exposed emergency history", reply.Code)
	}
}
