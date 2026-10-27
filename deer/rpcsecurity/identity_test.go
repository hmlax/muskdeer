package rpcsecurity

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"testing"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

func TestMethodIdentityRequiresVerifiedCertificateInsteadOfMetadata(t *testing.T) {
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("x-service-id", ServiceStrategy))
	if HasService(ctx, ServiceStrategy) {
		t.Fatal("metadata granted service identity")
	}
	certificate := &x509.Certificate{DNSNames: []string{ServiceAPI}}
	state := tls.ConnectionState{PeerCertificates: []*x509.Certificate{certificate}}
	withState := func(value tls.ConnectionState) context.Context {
		return peer.NewContext(ctx, &peer.Peer{AuthInfo: credentials.TLSInfo{State: value}})
	}
	if HasService(withState(state), ServiceAPI) {
		t.Fatal("unverified certificate granted service identity")
	}
	state.VerifiedChains = [][]*x509.Certificate{{certificate}}
	if !HasService(withState(state), ServiceAPI) || HasService(withState(state), ServiceStrategy) {
		t.Fatal("verified peer was not scoped to its exact certificate identity")
	}
}
