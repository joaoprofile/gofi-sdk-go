package httpx

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"github.com/joaoprofile/gofi-sdk-go/netx"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testPKI is a throwaway CA with helpers to issue leaf certificates.
type testPKI struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
	pem  []byte
}

func newTestPKI(t *testing.T) *testPKI {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &testPKI{cert: cert, key: key, pool: pool, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

// issue returns PEM cert and key for a leaf with the given serial and usage.
func (p *testPKI) issue(t *testing.T, serial int64, usage x509.ExtKeyUsage) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: "leaf"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, p.cert, &key.PublicKey, p.key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

// writeServerFiles writes a server key pair and the CA bundle into dir.
func (p *testPKI) writeServerFiles(t *testing.T, dir string, serial int64) (certFile, keyFile, caFile string) {
	t.Helper()
	certPEM, keyPEM := p.issue(t, serial, x509.ExtKeyUsageServerAuth)
	certFile, keyFile, caFile = filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key"), filepath.Join(dir, "ca.crt")
	require.NoError(t, os.WriteFile(certFile, certPEM, 0o600))
	require.NoError(t, os.WriteFile(keyFile, keyPEM, 0o600))
	require.NoError(t, os.WriteFile(caFile, p.pem, 0o600))
	return certFile, keyFile, caFile
}

// startTLSServer runs NewServer with cfg on a free port until the test ends.
func startTLSServer(t *testing.T, cfg *netx.TLSConfig) string {
	t.Helper()
	addr := freePort(t)
	ws := NewServer(&WSConfig{ServerPort: addr, TLS: cfg}).(*httpServer)
	ws.AddHandlers(&mockRouterHandler{routes: PublicRoutes("/", GET("/proto").To(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Proto", r.Proto)
		w.WriteHeader(http.StatusOK)
	}))})
	done := make(chan error, 1)
	go func() { done <- ws.ListenAndServe() }()
	t.Cleanup(func() {
		require.NoError(t, ws.Shutdown(context.Background()))
		require.NoError(t, <-done)
	})
	require.Eventually(t, func() bool {
		c, err := net.Dial("tcp", addr)
		if err == nil {
			_ = c.Close()
		}
		return err == nil
	}, 3*time.Second, 20*time.Millisecond)
	return "https://" + addr
}

func tlsClient(roots *x509.CertPool, certs ...tls.Certificate) *http.Client {
	return &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		ForceAttemptHTTP2: true,
		TLSClientConfig:   &tls.Config{RootCAs: roots, Certificates: certs, MinVersion: tls.VersionTLS12},
	}}
}

func TestTLS_ServesHTTP2WithFileCertificates(t *testing.T) {
	pki := newTestPKI(t)
	certFile, keyFile, _ := pki.writeServerFiles(t, t.TempDir(), 2)
	base := startTLSServer(t, &netx.TLSConfig{CertFile: certFile, KeyFile: keyFile})

	resp, err := tlsClient(pki.pool).Get(base + "/proto")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "HTTP/2.0", resp.Header.Get("X-Proto"))
	assert.GreaterOrEqual(t, resp.TLS.Version, uint16(tls.VersionTLS12))
}

func TestTLS_RejectsLegacyVersions(t *testing.T) {
	pki := newTestPKI(t)
	certFile, keyFile, _ := pki.writeServerFiles(t, t.TempDir(), 2)
	base := startTLSServer(t, &netx.TLSConfig{CertFile: certFile, KeyFile: keyFile})

	client := tlsClient(pki.pool)
	tr := client.Transport.(*http.Transport)
	tr.TLSClientConfig.MinVersion = tls.VersionTLS10
	tr.TLSClientConfig.MaxVersion = tls.VersionTLS11 // #nosec G402 -- asserting the server rejects it
	_, err := client.Get(base + "/proto")
	require.Error(t, err)
}

func TestTLS_MutualTLS(t *testing.T) {
	pki := newTestPKI(t)
	certFile, keyFile, caFile := pki.writeServerFiles(t, t.TempDir(), 2)
	base := startTLSServer(t, &netx.TLSConfig{CertFile: certFile, KeyFile: keyFile, ClientCAFile: caFile})

	_, err := tlsClient(pki.pool).Get(base + "/proto")
	require.Error(t, err, "a client without certificate must be rejected")

	other := newTestPKI(t)
	strangerPEM, strangerKey := other.issue(t, 3, x509.ExtKeyUsageClientAuth)
	stranger, err := tls.X509KeyPair(strangerPEM, strangerKey)
	require.NoError(t, err)
	_, err = tlsClient(pki.pool, stranger).Get(base + "/proto")
	require.Error(t, err, "a certificate from another CA must be rejected")

	clientPEM, clientKey := pki.issue(t, 4, x509.ExtKeyUsageClientAuth)
	clientCert, err := tls.X509KeyPair(clientPEM, clientKey)
	require.NoError(t, err)
	resp, err := tlsClient(pki.pool, clientCert).Get(base + "/proto")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestListenAndServe_RejectsH2CWithTLS(t *testing.T) {
	ws := NewServer(&WSConfig{ServerPort: freePort(t), H2C: true, TLS: &netx.TLSConfig{CertFile: "a", KeyFile: "b"}})
	err := ws.ListenAndServe()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "H2C")
}

func TestListenAndServe_InvalidTLSFails(t *testing.T) {
	ws := NewServer(&WSConfig{ServerPort: freePort(t), TLS: &netx.TLSConfig{CertFile: "/nonexistent.crt", KeyFile: "/nonexistent.key"}})
	require.Error(t, ws.ListenAndServe())
}
