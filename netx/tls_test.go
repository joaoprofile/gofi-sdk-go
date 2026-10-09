package netx

import (
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

func TestCertReloader_PicksUpRotatedFiles(t *testing.T) {
	pki := newTestPKI(t)
	dir := t.TempDir()
	certFile, keyFile, _ := pki.writeServerFiles(t, dir, 10)

	r, err := newCertReloader(certFile, keyFile)
	require.NoError(t, err)
	r.interval = 0
	serial := func() int64 {
		c, err := r.GetCertificate(nil)
		require.NoError(t, err)
		leaf, err := x509.ParseCertificate(c.Certificate[0])
		require.NoError(t, err)
		return leaf.SerialNumber.Int64()
	}
	assert.Equal(t, int64(10), serial())

	// A half-written rotation (cert without matching key) keeps the old pair.
	newCert, newKey := pki.issue(t, 11, x509.ExtKeyUsageServerAuth)
	require.NoError(t, os.WriteFile(certFile, newCert, 0o600))
	future := time.Now().Add(time.Minute)
	require.NoError(t, os.Chtimes(certFile, future, future))
	assert.Equal(t, int64(10), serial())

	require.NoError(t, os.WriteFile(keyFile, newKey, 0o600))
	require.NoError(t, os.Chtimes(keyFile, future, future))
	assert.Equal(t, int64(11), serial())
}

func TestCertReloader_ThrottlesChecks(t *testing.T) {
	pki := newTestPKI(t)
	certFile, keyFile, _ := pki.writeServerFiles(t, t.TempDir(), 10)
	r, err := newCertReloader(certFile, keyFile)
	require.NoError(t, err)
	r.interval = time.Hour

	require.NoError(t, os.Remove(certFile))
	c, err := r.GetCertificate(nil)
	require.NoError(t, err)
	assert.NotNil(t, c, "files are not stat'ed within the interval")
}

func TestServerTLSConfig(t *testing.T) {
	pki := newTestPKI(t)
	certFile, keyFile, caFile := pki.writeServerFiles(t, t.TempDir(), 2)

	cfg, err := serverTLSConfig(nil)
	require.NoError(t, err)
	assert.Nil(t, cfg)

	cfg, err = serverTLSConfig(&TLSConfig{CertFile: certFile, KeyFile: keyFile})
	require.NoError(t, err)
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
	assert.Equal(t, []string{"h2", "http/1.1"}, cfg.NextProtos)
	assert.Equal(t, tls.NoClientCert, cfg.ClientAuth)

	cfg, err = serverTLSConfig(&TLSConfig{CertFile: certFile, KeyFile: keyFile, ClientCAFile: caFile})
	require.NoError(t, err)
	assert.Equal(t, tls.RequireAndVerifyClientCert, cfg.ClientAuth)

	cfg, err = serverTLSConfig(&TLSConfig{CertFile: certFile, KeyFile: keyFile, ClientCAFile: caFile, ClientAuth: tls.VerifyClientCertIfGiven})
	require.NoError(t, err)
	assert.Equal(t, tls.VerifyClientCertIfGiven, cfg.ClientAuth)

	ready := &tls.Config{MinVersion: tls.VersionTLS10} // #nosec G402 -- asserting it is raised
	cfg, err = serverTLSConfig(&TLSConfig{Config: ready, CertFile: "ignored"})
	require.NoError(t, err)
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
	assert.Equal(t, uint16(tls.VersionTLS10), ready.MinVersion, "the caller's config is not mutated")

	for name, bad := range map[string]*TLSConfig{
		"missing key":        {CertFile: certFile},
		"missing files":      {CertFile: "/nonexistent.crt", KeyFile: "/nonexistent.key"},
		"verify without CA":  {CertFile: certFile, KeyFile: keyFile, ClientAuth: tls.RequireAndVerifyClientCert},
		"CA file without CA": {CertFile: certFile, KeyFile: keyFile, ClientCAFile: keyFile},
	} {
		_, err := serverTLSConfig(bad)
		assert.Error(t, err, name)
	}
}
