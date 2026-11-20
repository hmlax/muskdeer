package data

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/hmlax/muskdeer/deer/management/runtimeconfig"
	pb "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
)

type consoleTLSPeer struct {
	pb.UnimplementedConsoleServer
	calls atomic.Int32
}

func (s *consoleTLSPeer) ListAccounts(ctx context.Context, _ *pb.ListRequest) (*pb.AccountPage, error) {
	s.calls.Add(1)
	remote, ok := peer.FromContext(ctx)
	if !ok {
		return nil, status.Error(codes.PermissionDenied, "missing TLS peer")
	}
	info, ok := remote.AuthInfo.(credentials.TLSInfo)
	if !ok || len(info.State.VerifiedChains) == 0 || !slices.Contains(info.State.PeerCertificates[0].DNSNames, "operator.grpc") {
		return nil, status.Error(codes.PermissionDenied, "operator identity required")
	}
	// A control caller must receive an ambiguous transport outcome only once.
	return nil, status.Error(codes.Unavailable, "test peer closed the request")
}

func TestMuskdeerClientUsesVerifiedMTLSAndDoesNotReplay(t *testing.T) {
	directory := t.TempDir()
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	root := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "isolated-console-test"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, root, root, &rootKey.PublicKey, rootKey)
	require.NoError(t, err)
	caFile := filepath.Join(directory, "root.pem")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: rootDER,
	}), 0600))
	leaf := func(name string, serial int64) (tls.Certificate, string, string) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		cert := &x509.Certificate{
			SerialNumber: big.NewInt(serial),
			DNSNames:     []string{name},
			NotBefore:    root.NotBefore,
			NotAfter:     root.NotAfter,
			KeyUsage:     x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		}
		der, err := x509.CreateCertificate(rand.Reader, cert, root, &key.PublicKey, rootKey)
		require.NoError(t, err)
		private, err := x509.MarshalPKCS8PrivateKey(key)
		require.NoError(t, err)
		certFile, keyFile := filepath.Join(directory, name+".pem"), filepath.Join(directory, name+".key")
		certPEM, keyPEM := pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: der,
		}), pem.EncodeToMemory(&pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: private,
		})
		require.NoError(t, os.WriteFile(certFile, certPEM, 0600))
		require.NoError(t, os.WriteFile(keyFile, keyPEM, 0600))
		pair, err := tls.X509KeyPair(certPEM, keyPEM)
		require.NoError(t, err)
		return pair, certFile, keyFile
	}
	serverCert, _, _ := leaf("strategy.grpc", 2)
	_, clientCert, clientKey := leaf("operator.grpc", 3)
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: rootDER,
	})))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	})))
	implementation := &consoleTLSPeer{}
	pb.RegisterConsoleServer(server, implementation)
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); require.NoError(t, <-done) })
	settings := runtimeconfig.MuskdeerSettings{
		Enabled:         true,
		Endpoint:        listener.Addr().String(),
		ServerName:      "strategy.grpc",
		RootCertFile:    caFile,
		CertificateFile: clientCert,
		PrivateKeyFile:  clientKey,
		TimeoutMs:       3000,
	}
	client, err := OpenMuskdeerConnection(settings)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.conn.Close()) })
	_, err = client.Client().ListAccounts(t.Context(), &pb.ListRequest{})
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Equal(t, int32(1), implementation.calls.Load())
	_, wrongCert, wrongKey := leaf("admin.grpc", 4)
	settings.CertificateFile, settings.PrivateKeyFile = wrongCert, wrongKey
	_, err = OpenMuskdeerConnection(settings)
	require.Error(t, err, "admin HTTP identity cannot impersonate trusted operator")
	settings.ServerName = "execution.grpc"
	_, err = OpenMuskdeerConnection(settings)
	require.Error(t, err, "wrong target identity must fail closed")
}
