package strategygrpc

import (
	"context"
	"math"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
)

type publicationStore struct {
	calls   int
	token   string
	csrf    string
	request domain.AdminPublishStrategy
	err     error
}

func (s *publicationStore) Publish(_ context.Context, token, csrf string, request domain.AdminPublishStrategy) (domain.AdminPublicationReceipt, error) {
	s.calls++
	s.token, s.csrf, s.request = token, csrf, request
	return domain.AdminPublicationReceipt{
		ID:                    9007199254740993,
		ActorUserID:           1,
		StrategyID:            request.StrategyID,
		PreviousConfigVersion: request.ExpectedConfigVersion,
		ConfigVersion:         request.ExpectedConfigVersion + 1,
		PreviousRevision:      request.ExpectedRevision,
		Revision:              request.ExpectedRevision + 1,
		Source:                request.Source,
		Reason:                request.Reason,
		CreatedAt:             time.UnixMilli(1700000000000),
	}, s.err
}

func publicationRPCRequest() *strategyv1.PublishConfigurationRequest {
	return &strategyv1.PublishConfigurationRequest{
		Session:        &strategyv1.AdminSession{Token: testSessionToken},
		CsrfToken:      testSessionToken,
		IdempotencyKey: "publication-rpc-original-01",
		StrategyId:     9007199254740994,
		Input: &strategyv1.PublishConfigurationInput{
			ExpectedRevision:      9007199254740993,
			ExpectedConfigVersion: 1,
			MarketDataSource:      string(domain.MarketSourceOKX),
			Parameters:            creationRPCRequest().Input.Parameters,
			Reason:                "  explicit publication  ",
		},
	}
}

func TestPublicationGRPCPreservesTypedIdentityAndCurrentAuthorityFailures(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = verified(ctx, rpcsecurity.ServiceAPI)
	store := &publicationStore{}
	server, err := NewPublication(store)
	if err != nil {
		t.Fatal(err)
	}
	request := publicationRPCRequest()
	reply, err := server.PublishConfiguration(ctx, request)
	if err != nil || reply.StrategyId != request.StrategyId || reply.PreviousRevision != 9007199254740993 || reply.Revision != 9007199254740994 || store.token != testSessionToken || store.csrf != testSessionToken || store.request.Reason != "explicit publication" || store.request.Parameters.Grid.LowerPrice.String() != "98.123456789012345678" {
		t.Fatal("publication lost proof, optimistic identity or exact typed fields", err)
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
			err:  domain.ErrInvalidAdminPublication,
			code: codes.InvalidArgument,
		},
		{
			err:  domain.ErrAdminConflict,
			code: codes.Aborted,
		},
		{
			err:  domain.ErrAdminNotFound,
			code: codes.NotFound,
		},
	} {
		store.err = failure.err
		if _, err := server.PublishConfiguration(ctx, request); status.Code(err) != failure.code {
			t.Fatal("publication misclassified transaction failure", err)
		}
	}
}

func TestPublicationGRPCRejectsMissingProofUnknownFieldsAndUnsafeParameters(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	store := &publicationStore{}
	server, err := NewPublication(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.PublishConfiguration(verified(ctx, rpcsecurity.ServicePortfolio), publicationRPCRequest()); status.Code(err) != codes.PermissionDenied {
		t.Fatal("non-API publication accepted", err)
	}
	for _, mutate := range []func(*strategyv1.PublishConfigurationRequest){
		func(r *strategyv1.PublishConfigurationRequest) { r.Input = nil },
		func(r *strategyv1.PublishConfigurationRequest) { r.StrategyId = 0 },
		func(r *strategyv1.PublishConfigurationRequest) { r.Input.ExpectedRevision = math.MaxInt64 },
		func(r *strategyv1.PublishConfigurationRequest) { r.Input.ExpectedConfigVersion = 0 },
		func(r *strategyv1.PublishConfigurationRequest) {
			r.Input.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
		},
		func(r *strategyv1.PublishConfigurationRequest) {
			r.Input.Parameters.GetGrid().ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
		},
		func(r *strategyv1.PublishConfigurationRequest) {
			r.Input.Parameters.GetGrid().LowerPrice = "1e1000000000"
		},
		func(r *strategyv1.PublishConfigurationRequest) {
			r.Input.MarketDataSource = string(domain.MarketSourceBybit)
		},
		func(r *strategyv1.PublishConfigurationRequest) { r.CsrfToken = "" },
		func(r *strategyv1.PublishConfigurationRequest) { r.Session = nil },
	} {
		request := proto.CloneOf(publicationRPCRequest())
		mutate(request)
		if _, err := server.PublishConfiguration(verified(ctx, rpcsecurity.ServiceAPI), request); err == nil {
			t.Fatal("invalid publication reached storage")
		}
	}
	if store.calls != 0 {
		t.Fatal("invalid publication caused storage calls")
	}
}
