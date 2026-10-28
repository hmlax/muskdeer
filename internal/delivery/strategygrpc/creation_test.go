package strategygrpc

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
)

type creationStore struct {
	calls   int
	token   string
	csrf    string
	request domain.AdminCreateStrategy
	err     error
}

func (s *creationStore) Create(_ context.Context, token, csrf string, request domain.AdminCreateStrategy) (domain.AdminCreationReceipt, error) {
	s.calls++
	s.token, s.csrf, s.request = token, csrf, request
	return domain.AdminCreationReceipt{
		ID:            9007199254740993,
		ActorUserID:   1,
		StrategyID:    9007199254740994,
		Revision:      1,
		ConfigVersion: 1,
		Source:        request.Source,
		Reason:        request.Reason,
		CreatedAt:     time.UnixMilli(1700000000000),
	}, s.err
}

func creationRPCRequest() *strategyv1.CreateStrategyRequest {
	return &strategyv1.CreateStrategyRequest{
		Session:        &strategyv1.AdminSession{Token: testSessionToken},
		CsrfToken:      testSessionToken,
		IdempotencyKey: "creation-rpc-original-0001",
		Input: &strategyv1.CreateStrategyInput{
			InstanceKey:      "rpc-grid",
			AccountId:        "rpc-account",
			InstrumentId:     9007199254740993,
			CodeVersion:      "rpc-test",
			MarketDataSource: string(domain.MarketSourceOKX),
			MarketMode:       string(domain.MarketModeReplay),
			Reason:           "  explicit definition  ",
			Parameters: &commonv1.StrategyParameters{Template: &commonv1.StrategyParameters_Grid{Grid: &commonv1.GridParameters{
				LowerPrice:       "98.123456789012345678",
				UpperPrice:       "102",
				GridCount:        5,
				TotalBudget:      "50",
				PerOrderNotional: "10",
				InventoryLimit:   "20",
				MaxMarketAgeMs:   500,
			}}},
		},
	}
}

func TestCreationGRPCUsesTypedParametersAndPersistentAuthorityFailures(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = verified(ctx, rpcsecurity.ServiceAPI)
	store := &creationStore{}
	server, err := NewCreation(store)
	if err != nil {
		t.Fatal(err)
	}
	request := creationRPCRequest()
	reply, err := server.CreateStrategy(ctx, request)
	if err != nil || reply.StrategyId != 9007199254740994 || store.token != testSessionToken || store.csrf != testSessionToken || store.request.Reason != "explicit definition" || store.request.Parameters.Grid.LowerPrice.String() != "98.123456789012345678" {
		t.Fatal("creation lost proof, identity or exact typed parameters", err)
	}
	for _, failure := range []struct {
		err  error
		code codes.Code
	}{
		{
			err:  domain.ErrForbidden,
			code: codes.PermissionDenied,
		},
		{
			err:  domain.ErrUnauthenticated,
			code: codes.Unauthenticated,
		},
		{
			err:  domain.ErrInvalidAdminCreation,
			code: codes.InvalidArgument,
		},
		{
			err:  domain.ErrAdminConflict,
			code: codes.Aborted,
		},
	} {
		store.err = failure.err
		if _, err := server.CreateStrategy(ctx, request); status.Code(err) != failure.code {
			t.Fatal("creation misclassified persistent authority/transaction failure", err)
		}
	}
}

func TestCreationGRPCRejectsUnknownBusinessFieldsAndMissingProofBeforeStorage(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	store := &creationStore{}
	server, err := NewCreation(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.CreateStrategy(verified(ctx, rpcsecurity.ServicePortfolio), creationRPCRequest()); status.Code(err) != codes.PermissionDenied {
		t.Fatal("non-API creation accepted", err)
	}
	for _, mutate := range []func(*strategyv1.CreateStrategyRequest){
		func(r *strategyv1.CreateStrategyRequest) { r.Input = nil },
		func(r *strategyv1.CreateStrategyRequest) { r.Input.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01}) },
		func(r *strategyv1.CreateStrategyRequest) {
			r.Input.Parameters.GetGrid().ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
		},
		func(r *strategyv1.CreateStrategyRequest) { r.Input.Parameters.GetGrid().LowerPrice = "1e1000000000" },
		func(r *strategyv1.CreateStrategyRequest) { r.Input.MarketDataSource = string(domain.MarketSourceBybit) },
		func(r *strategyv1.CreateStrategyRequest) { r.CsrfToken = "" },
		func(r *strategyv1.CreateStrategyRequest) { r.Session = nil },
	} {
		request := proto.CloneOf(creationRPCRequest())
		mutate(request)
		if _, err := server.CreateStrategy(verified(ctx, rpcsecurity.ServiceAPI), request); err == nil {
			t.Fatal("invalid creation reached business storage")
		}
	}
	if store.calls != 0 {
		t.Fatal("invalid creation caused storage calls")
	}
}
