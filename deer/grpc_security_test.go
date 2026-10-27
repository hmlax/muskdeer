package deer

import (
	"context"
	"crypto/tls"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	probev1 "github.com/hmlax/muskdeer/api/probe/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestGRPCMutualTLSAuthenticatesServiceIdentity(t *testing.T) {
	t.Parallel()
	ca := testcert.New(t)
	serverCredentials := ca.Issue(t, "execution.grpc", false)
	probe := &identityProbe{observed: make(chan identitySnapshot, 1)}
	cfg := serviceConfig("authenticated-node")
	cfg.Name = "execution"
	cfg.GrpcSecurity = &configv1.GRPCServerSecurity{
		Mtls:                serverCredentials,
		AllowedPeerServices: []string{"api.grpc"},
	}
	service, _, err := NewDeer(t.Context(), cfg, ServiceSpec{
		RegisterGRPC: func(_ context.Context, _ *Deer, registrar grpc.ServiceRegistrar) error {
			probev1.RegisterArithmeticProbeServer(registrar, probe)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	runDeer(t, service)
	apiCredentials := ca.Issue(t, "api.grpc", false)
	accepted, err := rpcsecurity.Client(apiCredentials, "api.grpc", "execution.grpc")
	if err != nil {
		t.Fatal(err)
	}
	if err := callSecureProbe(t, service.GRPCAddress(), accepted); err != nil {
		t.Fatal(err)
	}
	if observed := <-probe.observed; observed.name != "execution" {
		t.Fatalf("authenticated service = %+v", observed)
	}

	otherCA := testcert.New(t)
	foreign := otherCA.Issue(t, "api.grpc", false)
	foreign.RootCertFile = ca.RootFile
	wrongTrust := ca.Issue(t, "api.grpc", false)
	wrongTrust.RootCertFile = otherCA.RootFile
	tests := []struct {
		name    string
		files   *configv1.MutualTLS
		self    string
		peer    string
		missing bool
	}{
		{
			name:  "trusted but unauthorized service",
			files: ca.Issue(t, "strategy.grpc", false),
			self:  "strategy.grpc",
			peer:  "execution.grpc",
		},
		{
			name:  "client signed by another CA",
			files: foreign,
			self:  "api.grpc",
			peer:  "execution.grpc",
		},
		{
			name:  "client does not trust server CA",
			files: wrongTrust,
			self:  "api.grpc",
			peer:  "execution.grpc",
		},
		{
			name:  "wrong server identity",
			files: apiCredentials,
			self:  "api.grpc",
			peer:  "market.grpc",
		},
		{
			name:  "expired client certificate",
			files: ca.Issue(t, "api.grpc", true),
			self:  "api.grpc",
			peer:  "execution.grpc",
		},
		{
			name:    "missing client certificate",
			files:   apiCredentials,
			self:    "api.grpc",
			peer:    "execution.grpc",
			missing: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			security, err := rpcsecurity.Client(test.files, test.self, test.peer)
			if err != nil {
				t.Fatal(err)
			}
			if test.missing {
				security.Certificates = nil
			}
			if err := callSecureProbe(t, service.GRPCAddress(), security); err == nil {
				t.Fatal("unauthenticated or unauthorized transport reached a business RPC")
			}
			select {
			case <-probe.observed:
				t.Fatal("rejected connection executed the handler")
			default:
			}
		})
	}
}

func TestGRPCSecurityRejectsWrongOwnIdentityAndWildcardPeer(t *testing.T) {
	t.Parallel()
	ca := testcert.New(t)
	files := ca.Issue(t, "strategy.grpc", false)
	if _, err := rpcsecurity.Client(files, "api.grpc", "execution.grpc"); err == nil {
		t.Fatal("client accepted another service's certificate")
	}
	if _, err := rpcsecurity.Server(&configv1.GRPCServerSecurity{
		Mtls:                files,
		AllowedPeerServices: []string{"*.grpc"},
	}, "strategy.grpc"); err == nil {
		t.Fatal("server accepted a wildcard peer allowlist")
	}
	if _, err := rpcsecurity.Server(&configv1.GRPCServerSecurity{
		Mtls:                files,
		AllowedPeerServices: []string{"api.grpc"},
	}, "execution.grpc"); err == nil {
		t.Fatal("server accepted another service's certificate")
	}
	if _, err := rpcsecurity.Server(&configv1.GRPCServerSecurity{
		Mtls:                ca.Issue(t, "execution.grpc", true),
		AllowedPeerServices: []string{"api.grpc"},
	}, "execution.grpc"); err == nil {
		t.Fatal("server accepted its expired certificate")
	}
}

func callSecureProbe(t *testing.T, address string, security *tls.Config) error {
	t.Helper()
	conn, err := grpc.NewClient("passthrough:///"+address,
		grpc.WithTransportCredentials(credentials.NewTLS(security)), grpc.WithDisableRetry())
	if err != nil {
		return err
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 400*time.Millisecond)
	defer cancel()
	_, err = probev1.NewArithmeticProbeClient(conn).Multiply(ctx, &probev1.MultiplyRequest{
		Price:    "0.1",
		Quantity: "0.2",
	})
	return err
}
