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
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

const cancellationHTTPPath = "/api/v1/strategies/9007199254740994/orders/9007199254740995/cancellations"
const cancellationReceiptHTTPPath = "/api/v1/strategies/9007199254740994/order-cancellations/9007199254740993"
const cancellationHTTPBody = `{"expected_order_revision":"9007199254740997","reason":"  cancel this selected order  "}`

type cancellationHTTPClient struct {
	strategyHTTPClient
	request *strategyv1.RequestOrderCancellationRequest
	mutate  func(*strategyv1.OrderCancellationReceipt)
}

func (*cancellationHTTPClient) receipt() *strategyv1.OrderCancellationReceipt {
	return &strategyv1.OrderCancellationReceipt{
		Id:                    9007199254740993,
		ActorUserId:           1,
		StrategyId:            9007199254740994,
		OrderId:               9007199254740995,
		ExpectedOrderRevision: 9007199254740997,
		Reason:                "cancel this selected order",
		State:                 string(domain.AdminCancellationAccepted),
		OutcomeCode:           domain.AdminCancellationPending,
		Revision:              1,
		CreatedAtMs:           1700000000000,
	}
}

func (c *cancellationHTTPClient) GetOrderCancellation(ctx context.Context, request *strategyv1.GetOrderCancellationRequest, _ ...grpc.CallOption) (*strategyv1.OrderCancellationReceipt, error) {
	c.record(ctx, request.Session)
	value := c.receipt()
	if c.mutate != nil {
		c.mutate(value)
	}
	return value, c.failure
}

func (c *cancellationHTTPClient) RequestOrderCancellation(ctx context.Context, request *strategyv1.RequestOrderCancellationRequest, _ ...grpc.CallOption) (*strategyv1.OrderCancellationReceipt, error) {
	c.record(ctx, request.Session)
	c.request = request
	value := c.receipt()
	if c.mutate != nil {
		c.mutate(value)
	}
	return value, c.failure
}

func cancellationHTTPFixture(t *testing.T, reader strategyv1.StrategyOrderCancellationQueryClient, writer strategyv1.StrategyOrderCancellationControlClient) *server.Hertz {
	t.Helper()
	routes, err := New(&authFixture{}, testOrigin, true)
	if err != nil {
		t.Fatal(err)
	}
	routes.cancellationQueries, routes.cancellationControls = reader, writer
	hertz := server.New()
	routes.Register(hertz)
	return hertz
}

func TestCancellationHTTPPreservesExactScopedAcceptanceAndTerminalResult(t *testing.T) {
	client := &cancellationHTTPClient{}
	hertz := cancellationHTTPFixture(t, client, client)
	reply := perform(hertz, "POST", cancellationHTTPPath, cancellationHTTPBody, controlHTTPHeaders()...)
	var value struct {
		ID       string `json:"id"`
		Strategy string `json:"strategy_id"`
		Order    string `json:"order_id"`
		Expected string `json:"expected_order_revision"`
		State    string `json:"state"`
	}
	if err := json.Unmarshal(reply.Body.Bytes(), &value); err != nil || reply.Code != 202 || value.ID != "9007199254740993" || value.Order != "9007199254740995" || value.Expected != "9007199254740997" || value.Strategy != "9007199254740994" || value.State != string(domain.AdminCancellationAccepted) {
		t.Fatal("scoped cancellation acceptance lost string identity or confirmation", err, reply.Code)
	}
	if string(reply.Header().Peek("Location")) != cancellationReceiptHTTPPath || client.request.CsrfToken != testSecret(2) || client.request.IdempotencyKey != "http-control-original-key" || client.request.Input.Reason != "cancel this selected order" || !client.bounded || client.token != testSecret(1) {
		t.Fatal("cancellation lost owned proof, deadline, normalization or result location")
	}
	if strings.Contains(reply.Body.String(), "completed_at_ms") || strings.Contains(reply.Body.String(), testSecret(1)) || string(reply.Header().Peek("Cache-Control")) != "no-store" {
		t.Fatal("accepted cancellation exposed completion, secrets or caching")
	}
	client.mutate = func(value *strategyv1.OrderCancellationReceipt) {
		value.State, value.OutcomeCode, value.Revision = string(domain.AdminCancellationSucceeded), domain.AdminCancellationTerminal, 3
		value.CompletedAtMs = proto.Int64(value.CreatedAtMs + 1)
		value.Observation = &strategyv1.OrderCancellationObservation{
			OrderRevision: value.ExpectedOrderRevision + 1,
			State:         string(domain.OrderStateFilled),
		}
	}
	for _, method := range []string{"GET", "POST"} {
		path, body := cancellationReceiptHTTPPath, ""
		if method == "POST" {
			path, body = cancellationHTTPPath, cancellationHTTPBody
		}
		if reply := perform(hertz, method, path, body, controlHTTPHeaders()...); reply.Code != 200 || !strings.Contains(reply.Body.String(), `"FILLED"`) || !strings.Contains(reply.Body.String(), `"9007199254740998"`) {
			t.Fatal("terminal replay/read lost the observed fill fact", reply.Code)
		}
	}
}

func TestCancellationHTTPRejectsAmbiguousBodiesAndMissingIndependentProof(t *testing.T) {
	client := &cancellationHTTPClient{}
	hertz := cancellationHTTPFixture(t, client, client)
	for _, body := range []string{
		`{}`, `null`, cancellationHTTPBody + `{}`,
		strings.Replace(cancellationHTTPBody, `"9007199254740997"`, `9007199254740997`, 1),
		strings.Replace(cancellationHTTPBody, `"9007199254740997"`, `"01"`, 1),
		strings.Replace(cancellationHTTPBody, `"9007199254740997"`, `"9223372036854775807"`, 1),
		strings.Replace(cancellationHTTPBody, `"reason":`, `"reason":"duplicate","reason":`, 1),
		strings.Replace(cancellationHTTPBody, `"reason":`, `"order_id":"1","reason":`, 1),
		strings.Replace(cancellationHTTPBody, `"reason":`, `"cancel_all":true,"reason":`, 1),
		strings.Replace(cancellationHTTPBody, `"  cancel this selected order  "`, `null`, 1),
		strings.Repeat(" ", maximumControlBytes+1),
	} {
		if reply := perform(hertz, "POST", cancellationHTTPPath, body, controlHTTPHeaders()...); reply.Code != 400 {
			t.Fatal("ambiguous cancellation body reached RPC", reply.Code)
		}
	}
	for index, expected := range []int{403, 401, 403, 400} {
		headers := controlHTTPHeaders()
		headers = append(headers[:index], headers[index+1:]...)
		if reply := perform(hertz, "POST", cancellationHTTPPath, cancellationHTTPBody, headers...); reply.Code != expected {
			t.Fatal("cancellation accepted missing independent proof", index, reply.Code)
		}
	}
	for _, path := range []string{cancellationHTTPPath + "?unexpected=1", strings.Replace(cancellationHTTPPath, "9007199254740995", "01", 1)} {
		if reply := perform(hertz, "POST", path, cancellationHTTPBody, controlHTTPHeaders()...); reply.Code != 400 {
			t.Fatal("ambiguous cancellation path reached RPC", reply.Code)
		}
	}
	for _, path := range []string{cancellationReceiptHTTPPath + "?unexpected=1", strings.Replace(cancellationReceiptHTTPPath, "9007199254740993", "01", 1)} {
		if reply := perform(hertz, "GET", path, "", controlHTTPHeaders()...); reply.Code != 400 {
			t.Fatal("ambiguous cancellation receipt query reached RPC", reply.Code)
		}
	}
	if client.calls != 0 {
		t.Fatal("invalid cancellation reached business RPC")
	}
	if reply := perform(cancellationHTTPFixture(t, client, nil), "POST", cancellationHTTPPath, cancellationHTTPBody, controlHTTPHeaders()...); reply.Code != 404 {
		t.Fatal("query-only management exposed cancellation writes", reply.Code)
	}
	if reply := perform(cancellationHTTPFixture(t, nil, nil), "GET", cancellationReceiptHTTPPath, "", controlHTTPHeaders()...); reply.Code != 404 {
		t.Fatal("disabled cancellation queries exposed receipts", reply.Code)
	}
}

func TestCancellationHTTPRejectsWrongSuccessfulReceiptsAndKeepsFixedFailures(t *testing.T) {
	client := &cancellationHTTPClient{}
	hertz := cancellationHTTPFixture(t, client, client)
	for _, mutate := range []func(*strategyv1.OrderCancellationReceipt){
		func(r *strategyv1.OrderCancellationReceipt) { r.StrategyId++ },
		func(r *strategyv1.OrderCancellationReceipt) { r.OrderId++ },
		func(r *strategyv1.OrderCancellationReceipt) { r.ExpectedOrderRevision++ },
		func(r *strategyv1.OrderCancellationReceipt) { r.Reason = "another request" },
		func(r *strategyv1.OrderCancellationReceipt) { r.CompletedAtMs = proto.Int64(r.CreatedAtMs) },
		func(r *strategyv1.OrderCancellationReceipt) { r.State = string(domain.AdminCancellationSucceeded) },
	} {
		client.mutate = mutate
		if reply := perform(hertz, "POST", cancellationHTTPPath, cancellationHTTPBody, controlHTTPHeaders()...); reply.Code != 503 || strings.Contains(reply.Body.String(), "order_id") {
			t.Fatal("wrong successful cancellation receipt escaped", reply.Code)
		}
	}
	client.mutate = func(r *strategyv1.OrderCancellationReceipt) { r.Id++ }
	if reply := perform(hertz, "GET", cancellationReceiptHTTPPath, "", controlHTTPHeaders()...); reply.Code != 503 {
		t.Fatal("receipt read accepted wrong identity", reply.Code)
	}
	for _, test := range []struct {
		code codes.Code
		want int
	}{
		{
			code: codes.Aborted,
			want: 409,
		},
		{
			code: codes.NotFound,
			want: 404,
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
		client.failure = status.Error(test.code, "private-cancellation-diagnostics")
		if reply := perform(hertz, "POST", cancellationHTTPPath, cancellationHTTPBody, controlHTTPHeaders()...); reply.Code != test.want || strings.Contains(reply.Body.String(), "private-cancellation-diagnostics") {
			t.Fatal("cancellation failure lost fixed classification", reply.Code)
		}
	}
}
