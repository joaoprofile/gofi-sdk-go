package obs

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// ErrInvalidTLS reports an unusable OTLP TLS setting; Init never falls back
// to plaintext.
var ErrInvalidTLS = errors.New("obs: invalid OTLP TLS configuration")

// tlsSettings reports whether any TLS field besides TLS itself is set.
func (c TeleConfig) tlsSettings() bool {
	return c.TLSConfig != nil || c.CAFile != "" || c.CertFile != "" || c.KeyFile != "" || c.ServerName != ""
}

// transportCredentials returns plaintext credentials when TLS is off and
// TLS credentials built from cfg otherwise.
func transportCredentials(cfg TeleConfig) (credentials.TransportCredentials, error) {
	if !cfg.TLS {
		if cfg.tlsSettings() {
			return nil, fmt.Errorf("%w: TLS settings need TLS=true", ErrInvalidTLS)
		}
		return insecure.NewCredentials(), nil
	}
	if cfg.TLSConfig != nil {
		return credentials.NewTLS(cfg.TLSConfig), nil
	}
	tlsCfg, err := clientTLSConfig(cfg)
	if err != nil {
		return nil, err
	}
	return credentials.NewTLS(tlsCfg), nil
}

// clientTLSConfig builds a TLS 1.2+ client configuration from the PEM files.
// A CA file replaces the system roots, as the OTel spec defines.
func clientTLSConfig(cfg TeleConfig) (*tls.Config, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: cfg.ServerName}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("%w: read CA file: %w", ErrInvalidTLS, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("%w: no PEM certificate in CA file %q", ErrInvalidTLS, cfg.CAFile)
		}
		tlsCfg.RootCAs = pool
	}
	if (cfg.CertFile == "") != (cfg.KeyFile == "") {
		return nil, fmt.Errorf("%w: client certificate needs both CertFile and KeyFile", ErrInvalidTLS)
	}
	if cfg.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("%w: load client certificate: %w", ErrInvalidTLS, err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}
	return tlsCfg, nil
}
