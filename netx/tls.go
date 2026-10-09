package netx

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"
)

// TLSConfig enables TLS on the httpx and grpcx servers. Config, when set, is
// used as is (cloned, with MinVersion raised to TLS 1.2); otherwise CertFile
// and KeyFile are loaded and reloaded on change, so certificate rotations
// (cert-manager, mounted secrets) apply without a restart.
type TLSConfig struct {
	// Config is a ready TLS configuration; it wins over the file fields.
	Config *tls.Config

	// CertFile and KeyFile are the PEM server certificate chain and key.
	CertFile string
	KeyFile  string

	// ClientCAFile is a PEM bundle of CAs trusted for client certificates
	// (mTLS). It is read once at startup.
	ClientCAFile string

	// ClientAuth defaults to tls.RequireAndVerifyClientCert when ClientCAFile
	// is set, tls.NoClientCert otherwise. With mTLS required, probes must
	// present a client certificate too; kubelet httpGet probes cannot, so use
	// tcpSocket/exec probes or a separate plain listener for them.
	ClientAuth tls.ClientAuthType
}

// certReloadInterval bounds how often the certificate files are stat'ed.
const certReloadInterval = time.Second

// ServerTLSConfig builds the hardened server tls.Config described by c, shared
// by httpx and grpcx: TLS 1.2+, forward-secret AEAD suites, certificate
// reload on file change and optional mTLS. ALPN offers h2 and http/1.1;
// grpcx narrows it to h2. A nil c returns nil.
func ServerTLSConfig(c *TLSConfig) (*tls.Config, error) {
	return serverTLSConfig(c)
}

// serverTLSConfig builds the server-side tls.Config: TLS 1.2+ (1.3 preferred
// by the handshake), ECDHE+AEAD suites only for TLS 1.2, ALPN h2 and
// http/1.1. A nil c returns nil (plain HTTP).
func serverTLSConfig(c *TLSConfig) (*tls.Config, error) {
	if c == nil {
		return nil, nil
	}
	if c.Config != nil {
		return hardenedClone(c.Config), nil
	}
	if c.CertFile == "" || c.KeyFile == "" {
		return nil, errors.New("netx: TLS requires Config or both CertFile and KeyFile")
	}

	reloader, err := newCertReloader(c.CertFile, c.KeyFile)
	if err != nil {
		return nil, err
	}
	cfg := &tls.Config{
		MinVersion:     tls.VersionTLS12,
		CipherSuites:   secureCipherSuites,
		NextProtos:     []string{"h2", "http/1.1"},
		GetCertificate: reloader.GetCertificate,
		ClientAuth:     c.ClientAuth,
	}
	if c.ClientCAFile != "" {
		pool, err := loadClientCAs(c.ClientCAFile)
		if err != nil {
			return nil, err
		}
		cfg.ClientCAs = pool
		if cfg.ClientAuth == tls.NoClientCert {
			cfg.ClientAuth = tls.RequireAndVerifyClientCert
		}
	}
	if cfg.ClientCAs == nil && (cfg.ClientAuth == tls.VerifyClientCertIfGiven || cfg.ClientAuth == tls.RequireAndVerifyClientCert) {
		return nil, errors.New("netx: TLS ClientAuth verifies client certificates but ClientCAFile is empty")
	}
	return cfg, nil
}

// hardenedClone copies a caller-supplied config, raising MinVersion to TLS
// 1.2 and defaulting ALPN to h2 and http/1.1.
func hardenedClone(src *tls.Config) *tls.Config {
	cfg := src.Clone()
	if cfg.MinVersion < tls.VersionTLS12 {
		cfg.MinVersion = tls.VersionTLS12
	}
	if len(cfg.NextProtos) == 0 {
		cfg.NextProtos = []string{"h2", "http/1.1"}
	}
	return cfg
}

// loadClientCAs reads the PEM bundle at path into a cert pool.
func loadClientCAs(path string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(path) // #nosec G304 -- operator-configured TLSConfig.ClientCAFile
	if err != nil {
		return nil, fmt.Errorf("netx: read client CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("netx: no certificate found in client CA file %s", path)
	}
	return pool, nil
}

// secureCipherSuites are the TLS 1.2 suites with forward secrecy and AEAD.
// TLS 1.3 suites are not configurable and always secure.
var secureCipherSuites = []uint16{
	tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
	tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
}

// certReloader serves a key pair and reloads it when either file changes.
// A failed reload keeps the previous pair, so a half-written rotation never
// takes the server down.
type certReloader struct {
	certFile, keyFile string
	interval          time.Duration

	mu        sync.Mutex
	cert      *tls.Certificate
	stamp     string // mtimes and sizes of both files
	lastCheck time.Time
}

func newCertReloader(certFile, keyFile string) (*certReloader, error) {
	r := &certReloader{certFile: certFile, keyFile: keyFile, interval: certReloadInterval}
	stamp, err := r.fileStamp()
	if err != nil {
		return nil, err
	}
	if err := r.load(stamp); err != nil {
		return nil, err
	}
	r.lastCheck = time.Now()
	return r, nil
}

// GetCertificate implements tls.Config.GetCertificate.
func (r *certReloader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if now := time.Now(); now.Sub(r.lastCheck) >= r.interval {
		r.lastCheck = now
		stamp, err := r.fileStamp()
		if err == nil && stamp != r.stamp {
			err = r.load(stamp)
		}
		if err != nil {
			slog.Warn("netx: TLS certificate reload failed, keeping the current one", slog.Any("error", err))
		}
	}
	return r.cert, nil
}

// fileStamp identifies the current version of both files. Stat follows
// symlinks, so atomic ..data swaps of mounted secrets are seen.
func (r *certReloader) fileStamp() (string, error) {
	var stamp string
	for _, f := range []string{r.certFile, r.keyFile} {
		fi, err := os.Stat(f)
		if err != nil {
			return "", fmt.Errorf("netx: TLS file: %w", err)
		}
		stamp += fmt.Sprintf("%d:%d;", fi.ModTime().UnixNano(), fi.Size())
	}
	return stamp, nil
}

func (r *certReloader) load(stamp string) error {
	cert, err := tls.LoadX509KeyPair(r.certFile, r.keyFile)
	if err != nil {
		return fmt.Errorf("netx: load TLS key pair: %w", err)
	}
	r.cert, r.stamp = &cert, stamp
	return nil
}
