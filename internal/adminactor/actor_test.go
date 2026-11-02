package adminactor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"testing"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"

	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestOnlyVerifiedAdminCanCarryAuditIdentityWithoutBrowserSecrets(t *testing.T) {
	actor := domain.AdminPrincipal{
		UserID:    9007199254740993,
		SessionID: 7,
		Username:  "operator-user",
		Role:      domain.AdminRoleViewer,
	}
	outbound := Outgoing(Bind(t.Context(), actor))
	md, _ := metadata.FromOutgoingContext(outbound)
	for _, name := range []string{"authorization", "cookie", "csrf_token", "session_token"} {
		if len(md.Get(name)) != 0 {
			t.Fatal("browser secret forwarded")
		}
	}
	for _, service := range []string{rpcsecurity.ServiceAdmin, rpcsecurity.ServiceOperator, rpcsecurity.ServiceAPI} {
		cert := &x509.Certificate{DNSNames: []string{service}}
		ctx := peer.NewContext(metadata.NewIncomingContext(t.Context(), md), &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{cert},
			VerifiedChains:   [][]*x509.Certificate{{cert}},
		}}})
		call, err := incoming(ctx)
		if service != rpcsecurity.ServiceAdmin {
			if err == nil {
				t.Fatal("non-admin peer asserted an actor")
			}
			continue
		}
		value, ok := From(call)
		if err != nil || !ok || value.UserID != actor.UserID || value.SessionID != actor.SessionID || value.Role != domain.AdminRoleViewer {
			t.Fatal("audit identity was rounded or reauthorized", err)
		}
		duplicate := md.Copy()
		duplicate.Append(userHeader, "1")
		if _, err := incoming(metadata.NewIncomingContext(ctx, duplicate)); err == nil {
			t.Fatal("ambiguous audit identity accepted")
		}
	}
	if _, err := incoming(metadata.NewIncomingContext(context.Background(), md)); err == nil {
		t.Fatal("unauthenticated metadata accepted")
	}
}
