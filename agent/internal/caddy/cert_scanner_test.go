package caddy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func createTestCert(t *testing.T, dir, filename string, domain string, expiry time.Time) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: domain},
		NotBefore:    time.Now().Add(-24 * time.Hour),
		NotAfter:     expiry,
		DNSNames:     []string{domain},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	certFile := filepath.Join(dir, filename)
	f, err := os.Create(certFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
}

func TestScanCertificates_ValidCert(t *testing.T) {
	dir := t.TempDir()
	certDir := filepath.Join(dir, "certificates")
	os.MkdirAll(certDir, 0755)
	expiry := time.Now().Add(30 * 24 * time.Hour)
	createTestCert(t, certDir, "cert.pem", "example.com", expiry)

	certs, err := ScanCertificates(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(certs) != 1 {
		t.Fatalf("expected 1 cert, got %d", len(certs))
	}
	if certs[0].Domain != "example.com" {
		t.Errorf("expected domain example.com, got %s", certs[0].Domain)
	}
	if certs[0].Issuer != "example.com" {
		t.Errorf("expected issuer example.com, got %s", certs[0].Issuer)
	}
}

func TestScanCertificates_ExpiredCert(t *testing.T) {
	dir := t.TempDir()
	certDir := filepath.Join(dir, "certificates")
	os.MkdirAll(certDir, 0755)
	expiry := time.Now().Add(-24 * time.Hour) // expired yesterday
	createTestCert(t, certDir, "expired.pem", "expired.com", expiry)

	certs, err := ScanCertificates(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(certs) != 1 {
		t.Fatalf("expected 1 cert, got %d", len(certs))
	}
	if certs[0].Domain != "expired.com" {
		t.Errorf("expected domain expired.com, got %s", certs[0].Domain)
	}
}

func TestScanCertificates_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	certs, err := ScanCertificates(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(certs) != 0 {
		t.Fatalf("expected 0 certs, got %d", len(certs))
	}
}

func TestScanCertificates_NonexistentDir(t *testing.T) {
	certs, err := ScanCertificates("/nonexistent/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(certs) != 0 {
		t.Fatalf("expected 0 certs, got %d", len(certs))
	}
}

func TestScanCertificates_MultipleCerts(t *testing.T) {
	dir := t.TempDir()
	certDir := filepath.Join(dir, "certificates")
	os.MkdirAll(certDir, 0755)
	createTestCert(t, certDir, "cert1.pem", "site1.com", time.Now().Add(30*24*time.Hour))
	createTestCert(t, certDir, "cert2.pem", "site2.com", time.Now().Add(60*24*time.Hour))

	certs, err := ScanCertificates(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(certs) != 2 {
		t.Fatalf("expected 2 certs, got %d", len(certs))
	}
}

func TestScanCertificates_NonPEMFile(t *testing.T) {
	dir := t.TempDir()
	certDir := filepath.Join(dir, "certificates")
	os.MkdirAll(certDir, 0755)
	os.WriteFile(filepath.Join(certDir, "readme.txt"), []byte("not a cert"), 0644)

	certs, err := ScanCertificates(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(certs) != 0 {
		t.Fatalf("expected 0 certs, got %d", len(certs))
	}
}

func TestParseCertFile_InvalidPEM(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "bad.pem"), []byte("not pem data"), 0644)

	cert, err := parseCertFile(filepath.Join(dir, "bad.pem"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cert != nil {
		t.Errorf("expected nil cert for invalid PEM, got %+v", cert)
	}
}

func TestParseCertFile_NoFile(t *testing.T) {
	cert, err := parseCertFile("/nonexistent/file.pem")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cert != nil {
		t.Errorf("expected nil cert for missing file, got %+v", cert)
	}
}
