package strategygrpc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
)

type controlStore struct {
	calls   int
	token   string
	csrf    string
	request domain.AdminControlRequest
	err     error
}

func (s *controlStore) Control(_ context.Context, token, csrf string, request domain.AdminControlRequest) (domain.AdminOperation, error) {
	s.calls++
	s.token, s.csrf, s.request = token, csrf, request
	return domain.AdminOperation{
		ID:        9007199254740993,
		State:     domain.AdminOperationAccepted,
		CreatedAt: time.UnixMilli(1700000000000),
	}, s.err
}

func (s *controlStore) Operation(_ context.Context, token string, id int64) (domain.AdminOperation, error) {
	s.calls++
	s.token = token
	return domain.AdminOperation{
		ID:          id,
		State:       domain.AdminOperationSucceeded,
		CreatedAt:   time.UnixMilli(1700000000000),
		CompletedAt: time.UnixMilli(1700000000001),
		Observation: &domain.AdminOperationObservation{
			StrategyRevision: 8,
			State:            domain.StrategyStateStopped,
		},
	}, s.err
}

func controlRequest() *strategyv1.ControlStrategyRequest {
	return &strategyv1.ControlStrategyRequest{
		Session:          &strategyv1.AdminSession{Token: testSessionToken},
		CsrfToken:        testSessionToken,
		IdempotencyKey:   "typed-control-request-0001",
		StrategyId:       1,
		ExpectedRevision: 2,
		Action:           string(domain.AdminControlStop),
		Reason:           "  controlled stop  ",
	}
}

func TestControlRPCRequiresAPIPeerAndCanonicalProofBeforeStorage(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	store := &controlStore{}
	server, err := NewControl(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, peer := range []context.Context{ctx, verified(ctx, rpcsecurity.ServicePortfolio)} {
		if _, err := server.ControlStrategy(peer, controlRequest()); status.Code(err) != codes.PermissionDenied || store.calls != 0 {
			t.Fatal("non-API peer reached control storage", err)
		}
	}
	api := verified(ctx, rpcsecurity.ServiceAPI)
	for _, mutate := range []func(*strategyv1.ControlStrategyRequest){
		func(v *strategyv1.ControlStrategyRequest) { v.CsrfToken = "" },
		func(v *strategyv1.ControlStrategyRequest) { v.Session.Token = "invalid" },
		func(v *strategyv1.ControlStrategyRequest) { v.ExpectedRevision = 0 },
		func(v *strategyv1.ControlStrategyRequest) { v.IdempotencyKey = "short" },
		func(v *strategyv1.ControlStrategyRequest) { v.Reason = strings.Repeat("x", 257) },
		func(v *strategyv1.ControlStrategyRequest) { v.Action = "strategy.close_position" },
		func(v *strategyv1.ControlStrategyRequest) { v.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01}) },
		func(v *strategyv1.ControlStrategyRequest) {
			v.Session.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
		},
	} {
		request := controlRequest()
		mutate(request)
		if _, err := server.ControlStrategy(api, request); err == nil || store.calls != 0 {
			t.Fatal("invalid proof/command reached control storage")
		}
	}
	result, err := server.ControlStrategy(api, controlRequest())
	if err != nil || store.calls != 1 || store.token != testSessionToken || store.csrf != testSessionToken || store.request.Reason != "controlled stop" || result.Id != 9007199254740993 || result.State != string(domain.AdminOperationAccepted) || result.CompletedAtMs != nil || result.Observation != nil {
		t.Fatal("accepted control fabricated completion or lost canonical proof", err)
	}
}

func TestControlRPCResultsRetainProofAndTypedPrivateFailures(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	api := verified(ctx, rpcsecurity.ServiceAPI)
	store := &controlStore{}
	server, err := NewControl(store)
	if err != nil {
		t.Fatal(err)
	}
	result, err := server.GetOperation(api, &strategyv1.GetOperationRequest{
		Session:     &strategyv1.AdminSession{Token: testSessionToken},
		OperationId: 9007199254740993,
	})
	if err != nil || result.GetCompletedAtMs() != 1700000000001 || result.Observation.GetState() != string(domain.StrategyStateStopped) || result.Observation.StrategyRevision != 8 {
		t.Fatal("terminal result lost its explicit persisted proof", err)
	}
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
			err:  domain.ErrUnauthenticated,
			code: codes.Unauthenticated,
		},
		{
			err:  errors.Join(domain.ErrAdminControlUnavailable, errors.New("private cause")),
			code: codes.Unavailable,
		},
	} {
		store.err = test.err
		request := proto.CloneOf(controlRequest())
		if _, err := server.ControlStrategy(api, request); status.Code(err) != test.code || strings.Contains(status.Convert(err).Message(), "private") {
			t.Fatal("control error lost its category or exposed private cause", err)
		}
	}
}
