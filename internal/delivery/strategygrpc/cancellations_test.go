package strategygrpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
)

type cancellationRPCStore struct {
	calls    int
	token    string
	csrf     string
	request  domain.AdminCancellationRequest
	deadline time.Time
	err      error
	mutate   func(*domain.AdminCancellationReceipt)
}

func (s *cancellationRPCStore) Request(ctx context.Context, token, csrf string, request domain.AdminCancellationRequest) (domain.AdminCancellationReceipt, error) {
	s.calls++
	s.token, s.csrf, s.request = token, csrf, request
	s.deadline, _ = ctx.Deadline()
	return s.result(9007199254740993, request.StrategyID, request.OrderID, request.ExpectedOrderRevision, request.Reason), s.err
}

func (s *cancellationRPCStore) Receipt(ctx context.Context, token string, strategyID, id int64) (domain.AdminCancellationReceipt, error) {
	s.calls++
	s.token = token
	s.deadline, _ = ctx.Deadline()
	return s.result(id, strategyID, 9007199254740995, 9007199254740997, "cancel this order"), s.err
}

func (s *cancellationRPCStore) result(id, strategyID, orderID, revision int64, reason string) domain.AdminCancellationReceipt {
	value := domain.AdminCancellationReceipt{
		ID:                    id,
		ActorUserID:           1,
		StrategyID:            strategyID,
		OrderID:               orderID,
		ExpectedOrderRevision: revision,
		Reason:                reason,
		State:                 domain.AdminCancellationAccepted,
		Outcome:               domain.AdminCancellationPending,
		Revision:              1,
		CreatedAt:             time.UnixMilli(1700000000000),
	}
	if s.mutate != nil {
		s.mutate(&value)
	}
	return value
}

func cancellationRPCRequest() *strategyv1.RequestOrderCancellationRequest {
	return &strategyv1.RequestOrderCancellationRequest{
		Session:        &strategyv1.AdminSession{Token: testSessionToken},
		CsrfToken:      testSessionToken,
		IdempotencyKey: "cancel-rpc-original-request-01",
		StrategyId:     9007199254740994,
		OrderId:        9007199254740995,
		Input: &strategyv1.OrderCancellationInput{
			ExpectedOrderRevision: 9007199254740997,
			Reason:                "  cancel this order  ",
		},
	}
}

func TestCancellationGRPCPreservesScopedConfirmationAndOriginalDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = verified(ctx, rpcsecurity.ServiceAPI)
	store := &cancellationRPCStore{}
	writer, err := NewCancellationControl(store)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewCancellationQuery(store)
	if err != nil {
		t.Fatal(err)
	}
	request := cancellationRPCRequest()
	reply, err := writer.RequestOrderCancellation(ctx, request)
	deadline, _ := ctx.Deadline()
	if err != nil || reply.Id != 9007199254740993 || reply.OrderId != request.OrderId || reply.ExpectedOrderRevision != request.Input.ExpectedOrderRevision || reply.CompletedAtMs != nil || reply.Observation != nil || store.token != testSessionToken || store.csrf != testSessionToken || store.request.Reason != "cancel this order" || store.deadline != deadline {
		t.Fatal("RPC lost exact order proof, presence or caller budget", err)
	}
	store.mutate = func(value *domain.AdminCancellationReceipt) {
		value.State, value.Outcome, value.Revision = domain.AdminCancellationFailed, domain.AdminCancellationRejected, 3
		value.CompletedAt = value.CreatedAt.Add(time.Millisecond)
		value.ObservedOrderRevision, value.ObservedOrderState = value.ExpectedOrderRevision, domain.OrderStateLive
	}
	read, err := reader.GetOrderCancellation(ctx, &strategyv1.GetOrderCancellationRequest{
		Session:        request.Session,
		StrategyId:     request.StrategyId,
		CancellationId: reply.Id,
	})
	if err != nil || read.State != string(domain.AdminCancellationFailed) || read.Observation == nil || read.Observation.State != string(domain.OrderStateLive) || read.CompletedAtMs == nil {
		t.Fatal("RPC lost a failed cancellation's remaining obligation", err)
	}
	encoded, err := proto.Marshal(read)
	if err != nil {
		t.Fatal(err)
	}
	var decoded strategyv1.OrderCancellationReceipt
	if err := proto.Unmarshal(encoded, &decoded); err != nil || !proto.Equal(read, &decoded) {
		t.Fatal("actual protobuf roundtrip lost receipt proof", err)
	}
}

func TestCancellationGRPCRejectsMissingIntentAndUnknownWireBeforeStorage(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = verified(ctx, rpcsecurity.ServiceAPI)
	store := &cancellationRPCStore{}
	writer, err := NewCancellationControl(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*strategyv1.RequestOrderCancellationRequest){
		func(r *strategyv1.RequestOrderCancellationRequest) { r.OrderId = 0 },
		func(r *strategyv1.RequestOrderCancellationRequest) { r.StrategyId = 0 },
		func(r *strategyv1.RequestOrderCancellationRequest) { r.Input = nil },
		func(r *strategyv1.RequestOrderCancellationRequest) { r.Input.ExpectedOrderRevision = 0 },
		func(r *strategyv1.RequestOrderCancellationRequest) { r.IdempotencyKey = "short" },
		func(r *strategyv1.RequestOrderCancellationRequest) {
			r.Input.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 1})
		},
		func(r *strategyv1.RequestOrderCancellationRequest) {
			r.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 1})
		},
	} {
		request := cancellationRPCRequest()
		change(request)
		if _, err := writer.RequestOrderCancellation(ctx, request); status.Code(err) != codes.InvalidArgument {
			t.Fatal("invalid/legacy cancellation intent reached storage", err)
		}
	}
	request := cancellationRPCRequest()
	request.CsrfToken = "invalid"
	if _, err := writer.RequestOrderCancellation(ctx, request); status.Code(err) != codes.PermissionDenied {
		t.Fatal("cancellation omitted independent CSRF", err)
	}
	request = cancellationRPCRequest()
	request.Session = nil
	if _, err := writer.RequestOrderCancellation(ctx, request); status.Code(err) != codes.Unauthenticated {
		t.Fatal("cancellation omitted current session", err)
	}
	if _, err := writer.RequestOrderCancellation(verified(t.Context(), rpcsecurity.ServiceAPI), cancellationRPCRequest()); status.Code(err) != codes.InvalidArgument {
		t.Fatal("unbounded cancellation reached storage", err)
	}
	if _, err := writer.RequestOrderCancellation(verified(ctx, rpcsecurity.ServicePortfolio), cancellationRPCRequest()); status.Code(err) != codes.PermissionDenied {
		t.Fatal("other-service peer requested cancellation", err)
	}
	if store.calls != 0 {
		t.Fatal("rejected cancellation reached business transaction")
	}
}

func TestCancellationGRPCRejectsMismatchedSuccessAndKeepsPublicErrorsFixed(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = verified(ctx, rpcsecurity.ServiceAPI)
	store := &cancellationRPCStore{}
	writer, err := NewCancellationControl(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*domain.AdminCancellationReceipt){
		func(r *domain.AdminCancellationReceipt) { r.OrderID++ },
		func(r *domain.AdminCancellationReceipt) { r.ExpectedOrderRevision++ },
		func(r *domain.AdminCancellationReceipt) { r.CompletedAt = r.CreatedAt },
		func(r *domain.AdminCancellationReceipt) { r.ObservedOrderState = domain.OrderStateCanceled },
	} {
		store.mutate = change
		if reply, err := writer.RequestOrderCancellation(ctx, cancellationRPCRequest()); status.Code(err) != codes.Unavailable || reply != nil {
			t.Fatal("mismatched successful receipt escaped RPC", err)
		}
	}
	store.mutate = nil
	for _, test := range []struct {
		err  error
		code codes.Code
	}{
		{
			err:  domain.ErrAdminConflict,
			code: codes.Aborted,
		},
		{
			err:  domain.ErrForbidden,
			code: codes.PermissionDenied,
		},
		{
			err:  domain.ErrAdminNotFound,
			code: codes.NotFound,
		},
		{
			err:  errors.New("private-cancellation-storage-failure"),
			code: codes.Unavailable,
		},
	} {
		store.err = test.err
		if reply, err := writer.RequestOrderCancellation(ctx, cancellationRPCRequest()); status.Code(err) != test.code || reply != nil || status.Convert(err).Message() == "private-cancellation-storage-failure" {
			t.Fatal("RPC error lost fixed public classification", err)
		}
	}
}
