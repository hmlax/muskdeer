package strategygrpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
)

const testSessionToken = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

type queryStore struct {
	calls int
	err   error
	token string
}

func (s *queryStore) List(_ context.Context, token string, _ domain.AdminPage) (domain.AdminStrategyPage, error) {
	s.calls++
	s.token = token
	return domain.AdminStrategyPage{}, s.err
}

func (s *queryStore) Detail(_ context.Context, token string, _ int64) (domain.AdminStrategyDetail, error) {
	s.calls++
	s.token = token
	return domain.AdminStrategyDetail{}, s.err
}

func (s *queryStore) Versions(_ context.Context, token string, _ int64, _ domain.AdminPage) (domain.AdminConfigPage, error) {
	s.calls++
	s.token = token
	return domain.AdminConfigPage{}, s.err
}

func TestManagementQueriesRequireAPIPeerAndBoundedCanonicalSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	store := &queryStore{}
	server, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	request := &strategyv1.ListStrategiesRequest{
		Session: &strategyv1.AdminSession{Token: testSessionToken},
		Limit:   1,
	}
	for _, denied := range []context.Context{
		ctx,
		metadata.NewIncomingContext(ctx, metadata.Pairs("x-service-id", rpcsecurity.ServiceAPI)),
		verified(ctx, rpcsecurity.ServiceStrategy),
		verified(ctx, rpcsecurity.ServicePortfolio),
	} {
		if _, err := server.ListStrategies(denied, request); status.Code(err) != codes.PermissionDenied || store.calls != 0 {
			t.Fatal("unverified/non-API peer reached management storage")
		}
	}
	api := verified(ctx, rpcsecurity.ServiceAPI)
	if _, err := server.ListStrategies(api, request); err != nil || store.calls != 1 || store.token != testSessionToken {
		t.Fatal("verified API did not forward the actual session", err)
	}
	if _, err := server.ListStrategies(verified(context.WithoutCancel(ctx), rpcsecurity.ServiceAPI), request); status.Code(err) != codes.InvalidArgument || store.calls != 1 {
		t.Fatal("unbounded query reached storage")
	}
	request.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
	if _, err := server.ListStrategies(api, request); status.Code(err) != codes.InvalidArgument || store.calls != 1 {
		t.Fatal("unknown request field accepted")
	}
	request.ProtoReflect().SetUnknown(nil)
	request.Session.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
	if _, err := server.ListStrategies(api, request); status.Code(err) != codes.Unauthenticated || store.calls != 1 {
		t.Fatal("unknown credential field accepted")
	}
}

func TestManagementAllMethodsRejectInvalidInputBeforeStorage(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	api := verified(ctx, rpcsecurity.ServiceAPI)
	store := &queryStore{}
	server, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []*strategyv1.ListStrategiesRequest{
		nil,
		{Limit: domain.MaxAdminPageSize + 1},
		{
			AfterId: -1,
			Limit:   1,
		},
	} {
		if _, err := server.ListStrategies(api, request); status.Code(err) != codes.InvalidArgument {
			t.Fatal("invalid list request reached storage", err)
		}
	}
	if _, err := server.GetStrategy(api, nil); status.Code(err) != codes.InvalidArgument {
		t.Fatal("nil detail accepted")
	}
	if _, err := server.ListConfigVersions(api, &strategyv1.ListConfigVersionsRequest{StrategyId: 1}); status.Code(err) != codes.InvalidArgument {
		t.Fatal("unbounded version list accepted")
	}
	if _, err := server.GetStrategy(api, &strategyv1.GetStrategyRequest{StrategyId: 1}); status.Code(err) != codes.Unauthenticated {
		t.Fatal("missing credential accepted")
	}
	if _, err := server.ListConfigVersions(api, &strategyv1.ListConfigVersionsRequest{
		Session:    &strategyv1.AdminSession{Token: "not-a-session"},
		StrategyId: 1,
		Limit:      1,
	}); status.Code(err) != codes.Unauthenticated || store.calls != 0 {
		t.Fatal("invalid credential reached storage")
	}
}

func TestManagementErrorsRemainTypedAndContainNoPrivateCause(t *testing.T) {
	for _, test := range []struct {
		err  error
		code codes.Code
	}{
		{
			err:  domain.ErrUnauthenticated,
			code: codes.Unauthenticated,
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
			err:  context.DeadlineExceeded,
			code: codes.DeadlineExceeded,
		},
		{
			err:  errors.New("private database cause"),
			code: codes.Unavailable,
		},
	} {
		if err := rpcError(test.err); status.Code(err) != test.code || status.Convert(err).Message() == test.err.Error() && test.code == codes.Unavailable {
			t.Fatal("public error category or redaction changed")
		}
	}
}

func verified(ctx context.Context, service string) context.Context {
	certificate := &x509.Certificate{DNSNames: []string{service}}
	return peer.NewContext(ctx, &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{certificate},
		VerifiedChains:   [][]*x509.Certificate{{certificate}},
	}}})
}
