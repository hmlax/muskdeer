// Package rpcsecurity binds gRPC transport trust to explicit service identities.
package rpcsecurity

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

const maximumPEMBytes = 1024 * 1024

// ValidateListener checks configuration without opening files or sockets.
// Unauthenticated plaintext is confined to literal loopback test listeners.
func ValidateListener(address, service string, cfg *configv1.GRPCServerSecurity) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return errors.New("gRPC listener requires TCP host:port")
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return errors.New("gRPC listener port is invalid")
	}
	if cfg == nil {
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			return errors.New("non-loopback gRPC listeners require mutual TLS")
		}
		return nil
	}
	return validateServer(service, cfg)
}

// Server loads a private server identity and a CA, requiring authenticated
// clients whose exact DNS SAN appears in allowed_peer_services. No wildcard or
// metadata/header identity can grant transport access.
func Server(cfg *configv1.GRPCServerSecurity, service string) (*tls.Config, error) {
	if err := validateServer(service, cfg); err != nil {
		return nil, err
	}
	certificate, roots, err := load(cfg.Mtls, service)
	if err != nil {
		return nil, err
	}
	if now := time.Now(); now.Before(certificate.Leaf.NotBefore) || !now.Before(certificate.Leaf.NotAfter) {
		return nil, errors.New("gRPC server certificate is not currently valid")
	}
	allowed := slices.Clone(cfg.AllowedPeerServices)
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
		ClientCAs:    roots,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.VerifiedChains) == 0 || len(state.PeerCertificates) == 0 {
				return errors.New("gRPC requires a verified client certificate")
			}
			for _, service := range allowed {
				if slices.Contains(state.PeerCertificates[0].DNSNames, service) {
					return nil
				}
			}
			return errors.New("gRPC client service is not allowed")
		},
	}, nil
}

// Client loads the caller's exact service identity and verifies a peer by CA and
// exact DNS SAN, independently of the endpoint address returned by discovery.
func Client(cfg *configv1.MutualTLS, service, peer string) (*tls.Config, error) {
	if !validService(service) || !validService(peer) {
		return nil, errors.New("gRPC requires explicit service DNS names ending in .grpc")
	}
	certificate, roots, err := load(cfg, service)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
		RootCAs:      roots,
		ServerName:   peer,
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.VerifiedChains) == 0 || len(state.PeerCertificates) == 0 || !slices.Contains(state.PeerCertificates[0].DNSNames, peer) {
				return errors.New("gRPC server has no exact expected service identity")
			}
			return nil
		},
	}, nil
}

func validateServer(service string, cfg *configv1.GRPCServerSecurity) error {
	if !validService(service) || cfg == nil || cfg.Mtls == nil || len(cfg.AllowedPeerServices) == 0 || len(cfg.AllowedPeerServices) > 64 {
		return errors.New("gRPC mutual TLS requires identity, certificate files and 1..64 allowed peer services")
	}
	for _, peer := range cfg.AllowedPeerServices {
		if !validService(peer) {
			return errors.New("gRPC peer allowlist requires exact service DNS names ending in .grpc")
		}
	}
	return validateFiles(cfg.Mtls)
}

func validService(service string) bool {
	if !strings.HasSuffix(service, ".grpc") || len(service) > 253 {
		return false
	}
	for _, label := range strings.Split(service, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if ch != '-' && (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') {
				return false
			}
		}
	}
	return true
}

func validateFiles(cfg *configv1.MutualTLS) error {
	if cfg == nil || cfg.CertificateFile == "" || cfg.PrivateKeyFile == "" || cfg.RootCertFile == "" {
		return errors.New("gRPC mutual TLS requires certificate, private key and root certificate paths")
	}
	return nil
}

func load(cfg *configv1.MutualTLS, service string) (tls.Certificate, *x509.CertPool, error) {
	if err := validateFiles(cfg); err != nil {
		return tls.Certificate{}, nil, err
	}
	certificatePEM, err := readPEM(cfg.CertificateFile)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	keyPEM, err := readPEM(cfg.PrivateKeyFile)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	certificate, err := tls.X509KeyPair(certificatePEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("gRPC certificate/key pair: %w", err)
	}
	if certificate.Leaf == nil || !slices.Contains(certificate.Leaf.DNSNames, service) {
		return tls.Certificate{}, nil, errors.New("gRPC certificate does not contain its exact service identity")
	}
	rootPEM, err := readPEM(cfg.RootCertFile)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(rootPEM) {
		return tls.Certificate{}, nil, errors.New("gRPC root file contains no certificates")
	}
	return certificate, roots, nil
}

func readPEM(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open gRPC credential file: %w", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maximumPEMBytes+1))
	if err := errors.Join(readErr, file.Close()); err != nil {
		return nil, fmt.Errorf("read gRPC credential file: %w", err)
	}
	if len(data) > maximumPEMBytes {
		return nil, errors.New("gRPC credential file exceeds 1 MiB")
	}
	return data, nil
}
