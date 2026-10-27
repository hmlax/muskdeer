// Package testcert creates ephemeral credentials for local transport tests only.
package testcert

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

// Authority owns an in-memory test signing key. It is not a production issuer.
type Authority struct {
	certificate *x509.Certificate
	key         ed25519.PrivateKey
	RootFile    string
	serial      int64
}

// New creates an isolated, temporary signing authority for tests.
func New(t *testing.T) *Authority {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "muskdeer test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, public, private)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	rootFile := filepath.Join(t.TempDir(), "ca.pem")
	writePEM(t, rootFile, "CERTIFICATE", der)
	return &Authority{
		certificate: certificate,
		key:         private,
		RootFile:    rootFile,
		serial:      1,
	}
}

// Issue creates a temporary service certificate and a private key.
func (ca *Authority) Issue(t *testing.T, service string, expired bool) *configv1.MutualTLS {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca.serial++
	expires := time.Now().Add(time.Hour)
	if expired {
		expires = time.Now().Add(-time.Minute)
	}
	certificate := &x509.Certificate{
		SerialNumber: big.NewInt(ca.serial),
		DNSNames:     []string{service},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     expires,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, certificate, ca.certificate, public, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	key, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	files := &configv1.MutualTLS{
		CertificateFile: filepath.Join(t.TempDir(), "certificate.pem"),
		PrivateKeyFile:  filepath.Join(t.TempDir(), "key.pem"),
		RootCertFile:    ca.RootFile,
	}
	writePEM(t, files.CertificateFile, "CERTIFICATE", der)
	writePEM(t, files.PrivateKeyFile, "PRIVATE KEY", key)
	return files
}

func writePEM(t *testing.T, path, kind string, der []byte) {
	t.Helper()
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{
		Type:  kind,
		Bytes: der,
	}), 0600); err != nil {
		t.Fatal(err)
	}
}
