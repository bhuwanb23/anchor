package caddy

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CertInfo contains parsed information about a certificate.
type CertInfo struct {
	Domain string
	Expiry time.Time
	Issuer string
}

// ScanCertificates scans the Caddy certificate directory for PEM files
// and returns information about each certificate found.
func ScanCertificates(certDir string) ([]CertInfo, error) {
	certsDir := filepath.Join(certDir, "certificates")
	if _, err := os.Stat(certsDir); os.IsNotExist(err) {
		return nil, nil
	}

	var certs []CertInfo

	err := filepath.Walk(certsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}

		ext := filepath.Ext(path)
		if ext != ".crt" && ext != ".pem" {
			return nil
		}

		certInfo, err := parseCertFile(path)
		if err != nil {
			return nil
		}
		if certInfo != nil {
			certs = append(certs, *certInfo)
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walk certificate directory: %w", err)
	}

	return certs, nil
}

// parseCertFile reads a PEM certificate file and extracts domain, expiry, and issuer.
func parseCertFile(path string) (*CertInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, nil
	}

	if block.Type != "CERTIFICATE" {
		return nil, nil
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil
	}

	domain := cert.Subject.CommonName
	if domain == "" {
		domain = filepath.Base(path)
	}

	return &CertInfo{
		Domain: domain,
		Expiry: cert.NotAfter,
		Issuer: cert.Issuer.CommonName,
	}, nil
}
