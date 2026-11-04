package strategygrpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/deer/pb/strategy/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func TestEmergencyGRPCKeepsConfirmationAcceptanceAndNativeDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = verified(ctx, rpcsecurity.ServiceAPI)
	store := &emergencyRPCFixture{}
	control, err := NewEmergencyControl(store)
	if err != nil {
		t.Fatal(err)
	}
	request := emergencyRPCRequest()
	accepted, err := control.RequestEmergencyCancellation(ctx, request)
	deadline, _ := ctx.Deadline()
	if err != nil || accepted.Id != 9007199254740993 || accepted.StrategyId != request.StrategyId || accepted.ExpectedRiskRevision != request.Input.ExpectedRiskRevision ||
		accepted.RiskRevision != request.Input.ExpectedRiskRevision+1 || store.deadline != deadline || store.command.Scope != domain.AdminEmergencyCancelScopeStrategy ||
		store.command.Reason != "confirmed strategy scope" || store.csrf != testSessionToken || store.token != testSessionToken {
		t.Fatal("emergency RPC changed scoped confirmation or caller budget", err)
	}
	data, err := proto.Marshal(accepted)
	if err != nil {
		t.Fatal(err)
	}
	var decoded strategyv1.EmergencyCancellationAcceptance
	if err := proto.Unmarshal(data, &decoded); err != nil || !proto.Equal(accepted, &decoded) {
		t.Fatal("protobuf roundtrip lost exact emergency identity", err)
	}
}

func TestEmergencyGRPCRejectsScopeExpansionUnknownInputAndUnverifiedPeer(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = verified(ctx, rpcsecurity.ServiceAPI)
	store := &emergencyRPCFixture{}
	control, err := NewEmergencyControl(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*strategyv1.RequestEmergencyCancellationRequest){
		func(r *strategyv1.RequestEmergencyCancellationRequest) { r.StrategyId = 0 },
		func(r *strategyv1.RequestEmergencyCancellationRequest) { r.Input = nil },
		func(r *strategyv1.RequestEmergencyCancellationRequest) { r.Input.Scope = "account" },
		func(r *strategyv1.RequestEmergencyCancellationRequest) { r.Input.ExpectedRiskRevision = 0 },
		func(r *strategyv1.RequestEmergencyCancellationRequest) { r.IdempotencyKey = "short" },
		func(r *strategyv1.RequestEmergencyCancellationRequest) {
			r.Input.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 1})
		},
		func(r *strategyv1.RequestEmergencyCancellationRequest) {
			r.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 1})
		},
	} {
		request := emergencyRPCRequest()
		change(request)
		if _, err := control.RequestEmergencyCancellation(ctx, request); status.Code(err) != codes.InvalidArgument {
			t.Fatal("invalid emergency intent reached storage", err)
		}
	}
	request := emergencyRPCRequest()
	request.CsrfToken = "missing"
	if _, err := control.RequestEmergencyCancellation(ctx, request); status.Code(err) != codes.PermissionDenied {
		t.Fatal("emergency mutation omitted independent CSRF", err)
	}
	if _, err := control.RequestEmergencyCancellation(verified(ctx, rpcsecurity.ServicePortfolio), emergencyRPCRequest()); status.Code(err) != codes.PermissionDenied {
		t.Fatal("non-API peer reached emergency mutation", err)
	}
	if _, err := control.RequestEmergencyCancellation(verified(t.Context(), rpcsecurity.ServiceAPI), emergencyRPCRequest()); status.Code(err) != codes.InvalidArgument {
		t.Fatal("unbounded emergency request reached storage", err)
	}
	if store.calls != 0 {
		t.Fatal("rejected emergency request reached transaction")
	}
}

func TestEmergencyGRPCRejectsMismatchedResultsAndHidesPrivateErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = verified(ctx, rpcsecurity.ServiceAPI)
	store := &emergencyRPCFixture{}
	control, err := NewEmergencyControl(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*domain.AdminEmergencyCancelAcceptance){
		func(r *domain.AdminEmergencyCancelAcceptance) { r.StrategyID++ },
		func(r *domain.AdminEmergencyCancelAcceptance) { r.RiskRevision++ },
		func(r *domain.AdminEmergencyCancelAcceptance) { r.Scope = "account" },
		func(r *domain.AdminEmergencyCancelAcceptance) { r.CreatedAt = r.CreatedAt.Add(time.Nanosecond) },
	} {
		store.mutate = change
		if reply, err := control.RequestEmergencyCancellation(ctx, emergencyRPCRequest()); status.Code(err) != codes.Unavailable || reply != nil {
			t.Fatal("mismatched emergency acceptance escaped RPC", err)
		}
	}
	store.mutate = nil
	store.failure = errors.New("private-emergency-database-address")
	if reply, err := control.RequestEmergencyCancellation(ctx, emergencyRPCRequest()); status.Code(err) != codes.Unavailable || reply != nil || status.Convert(err).Message() == store.failure.Error() {
		t.Fatal("emergency RPC exposed private storage failure", err)
	}
}

type emergencyRPCFixture struct {
	calls    int
	command  domain.AdminEmergencyCancelRequest
	deadline time.Time
	token    string
	csrf     string
	failure  error
	mutate   func(*domain.AdminEmergencyCancelAcceptance)
}

func (s *emergencyRPCFixture) Request(ctx context.Context, token, csrf string, command domain.AdminEmergencyCancelRequest) (domain.AdminEmergencyCancelAcceptance, error) {
	s.calls++
	s.command, s.token, s.csrf = command, token, csrf
	s.deadline, _ = ctx.Deadline()
	value := domain.AdminEmergencyCancelAcceptance{
		ID:                   9007199254740993,
		ActorUserID:          1,
		StrategyID:           command.StrategyID,
		Scope:                command.Scope,
		ExpectedRiskRevision: command.ExpectedRiskRevision,
		RiskRevision:         command.ExpectedRiskRevision + 1,
		Reason:               command.Reason,
		CreatedAt:            time.UnixMilli(1700000000000),
	}
	if s.mutate != nil {
		s.mutate(&value)
	}
	return value, s.failure
}

func emergencyRPCRequest() *strategyv1.RequestEmergencyCancellationRequest {
	return &strategyv1.RequestEmergencyCancellationRequest{
		Session:        &strategyv1.AdminSession{Token: testSessionToken},
		CsrfToken:      testSessionToken,
		IdempotencyKey: "emergency-rpc-confirmation-0001",
		StrategyId:     9007199254740995,
		Input: &strategyv1.EmergencyCancellationInput{
			Scope:                string(domain.AdminEmergencyCancelScopeStrategy),
			ExpectedRiskRevision: 9007199254740997,
			Reason:               "  confirmed strategy scope  ",
		},
	}
}
