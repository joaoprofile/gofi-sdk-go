package msq

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
)

// TLSConfig customizes a broker TLS connection. The zero value verifies the
// server against the system roots.
type TLSConfig struct {
	// CAFile is a PEM bundle of CAs trusted for the server certificate, in
	// addition to the system roots.
	CAFile string
	// CertFile and KeyFile are the PEM client certificate and key (mTLS).
	CertFile string
	KeyFile  string
	// ServerName overrides the name verified in the server certificate (and
	// sent as SNI); empty uses the dialed host.
	ServerName string
	// InsecureSkipVerify disables server certificate verification. Never in
	// production: Insecure reports it and gofi refuses it there.
	InsecureSkipVerify bool
}

func (t TLSConfig) configured() bool {
	return t.CAFile != "" || t.CertFile != "" || t.KeyFile != "" || t.ServerName != "" || t.InsecureSkipVerify
}

// Config builds a TLS 1.2+ client configuration, loading the CA bundle and
// the client key pair from their files.
func (t TLSConfig) Config() (*tls.Config, error) {
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: t.ServerName,
		// #nosec G402 -- explicit opt-in for development; ProviderConfig.Insecure
		// reports it so production guards can refuse it.
		InsecureSkipVerify: t.InsecureSkipVerify,
	}
	if t.CAFile != "" {
		pem, err := os.ReadFile(t.CAFile)
		if err != nil {
			return nil, fmt.Errorf("msq: tls: read CA file: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("msq: tls: no PEM certificate in CA file %q", t.CAFile)
		}
		cfg.RootCAs = pool
	}
	if (t.CertFile == "") != (t.KeyFile == "") {
		return nil, errors.New("msq: tls: client certificate needs both CertFile and KeyFile")
	}
	if t.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("msq: tls: load client certificate: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}
