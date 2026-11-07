package console

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	pb "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
)

func operatorContext(ctx context.Context, service string) context.Context {
	certificate := &x509.Certificate{DNSNames: []string{service}}
	return peer.NewContext(ctx, &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{certificate},
		VerifiedChains:   [][]*x509.Certificate{{certificate}},
	}}})
}

func TestConsoleRequiresVerifiedOperatorInsteadOfCallerMetadata(t *testing.T) {
	t.Parallel()
	server := &Server{} // No database: denial must happen before any data access.
	actor := &pb.Actor{Subject: "admin:1"}
	for _, ctx := range []context.Context{
		t.Context(),
		metadata.NewIncomingContext(t.Context(), metadata.Pairs("service", "operator.grpc", "tenant-id", "0")),
		operatorContext(t.Context(), rpcsecurity.ServiceStrategy),
	} {
		if _, err := server.ListAccounts(ctx, &pb.ListRequest{Actor: actor}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("ListAccounts(untrusted context) = %v, want PermissionDenied before database", err)
		}
	}
}

func TestMerchantScopeCannotBecomeGlobalWhenEmptyOrAmbiguous(t *testing.T) {
	t.Parallel()
	server := &Server{}
	ctx := operatorContext(t.Context(), rpcsecurity.ServiceOperator)
	for _, ids := range [][]string{nil, {}, {""}, {"himalaya/1/1", "himalaya/1/1"}} {
		actor := &pb.Actor{
			Subject:           "admin:2",
			TenantId:          7,
			AllowedAccountIds: ids,
		}
		checks := []func() error{
			func() error { _, err := server.ListAccounts(ctx, &pb.ListRequest{Actor: actor}); return err },
			func() error { _, err := server.ListInstruments(ctx, &pb.ListRequest{Actor: actor}); return err },
			func() error { _, err := server.GetOverview(ctx, &pb.ActorRequest{Actor: actor}); return err },
			func() error { _, err := server.ListOrderRecords(ctx, &pb.RecordListRequest{Actor: actor}); return err },
			func() error { _, err := server.ListCancellations(ctx, &pb.RecordListRequest{Actor: actor}); return err },
			func() error {
				_, err := server.ListSyntheticRecords(ctx, &pb.RecordListRequest{Actor: actor})
				return err
			},
		}
		for _, check := range checks {
			if err := check(); status.Code(err) != codes.PermissionDenied {
				t.Fatalf("merchant scope %v = %v, want PermissionDenied before database", ids, err)
			}
		}
	}
}

func TestConsoleControlsRemainSeparateFromReadIdentity(t *testing.T) {
	t.Parallel()
	server := &Server{}
	ctx := operatorContext(t.Context(), rpcsecurity.ServiceOperator)
	actor := &pb.Actor{
		Subject:   "admin:1",
		RequestId: "550e8400-e29b-41d4-a716-446655440000",
	}
	if err := server.authorize(ctx, actor, false); err != nil {
		t.Fatal(err)
	}
	if err := server.authorize(ctx, actor, true); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("write without controls opt-in = %v, want PermissionDenied", err)
	}
}
