package obs

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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	colltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// testPKI holds PEM files for a CA, a collector certificate for 127.0.0.1
// and collector.test, and a client certificate, all signed by the CA.
type testPKI struct {
	caFile, clientCert, clientKey string
	caPool                        *x509.CertPool
	server                        tls.Certificate
}

func newTestPKI(t *testing.T) testPKI {
	t.Helper()
	dir := t.TempDir()
	caKey := newKey(t)
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	require.NoError(t, err)
	caCert, err := x509.ParseCertificate(caDER)
	require.NoError(t, err)

	issue := func(serial int64, usage x509.ExtKeyUsage, dns []string, ips []net.IP) ([]byte, *ecdsa.PrivateKey) {
		key := newKey(t)
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(serial),
			Subject:      pkix.Name{CommonName: "leaf"},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{usage},
			DNSNames:     dns,
			IPAddresses:  ips,
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
		require.NoError(t, err)
		return der, key
	}

	srvDER, srvKey := issue(2, x509.ExtKeyUsageServerAuth, []string{"collector.test"}, []net.IP{net.IPv4(127, 0, 0, 1)})
	server, err := tls.X509KeyPair(certPEM(srvDER), keyPEM(t, srvKey))
	require.NoError(t, err)
	cliDER, cliKey := issue(3, x509.ExtKeyUsageClientAuth, nil, nil)

	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	p := testPKI{
		caFile:     writeFile(t, dir, "ca.pem", certPEM(caDER)),
		clientCert: writeFile(t, dir, "client.pem", certPEM(cliDER)),
		clientKey:  writeFile(t, dir, "client-key.pem", keyPEM(t, cliKey)),
		caPool:     pool,
		server:     server,
	}
	return p
}

func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return key
}

func certPEM(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func keyPEM(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
}

func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

// startTLSCollector runs the fake collector behind TLS that requires and
// verifies a client certificate signed by the test CA.
func startTLSCollector(t *testing.T, p testPKI) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{p.server},
		ClientCAs:    p.caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	})))
	colltrace.RegisterTraceServiceServer(srv, fakeTraceService{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis.Addr().String()
}

// exportOnce sends one trace export over credentials built from cfg.
func exportOnce(t *testing.T, cfg TeleConfig) error {
	t.Helper()
	creds, err := transportCredentials(cfg)
	require.NoError(t, err)
	conn, err := grpc.NewClient(cfg.CollectorAddr, grpc.WithTransportCredentials(creds))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = colltrace.NewTraceServiceClient(conn).Export(ctx, &colltrace.ExportTraceServiceRequest{})
	return err
}

func TestTLS_MutualTLSExport(t *testing.T) {
	p := newTestPKI(t)
	addr := startTLSCollector(t, p)
	base := TeleConfig{CollectorAddr: addr, TLS: true, CAFile: p.caFile}

	withCert := base
	withCert.CertFile, withCert.KeyFile = p.clientCert, p.clientKey
	assert.NoError(t, exportOnce(t, withCert), "mTLS with the client certificate")

	assert.Error(t, exportOnce(t, base), "the collector requires a client certificate")

	sni := withCert
	sni.CollectorAddr = "dns:///" + addr
	sni.ServerName = "collector.test"
	assert.NoError(t, exportOnce(t, sni), "ServerName verifies the collector name")

	wrongName := withCert
	wrongName.ServerName = "other.test"
	assert.Error(t, exportOnce(t, wrongName), "the certificate does not match ServerName")

	systemRoots := TeleConfig{CollectorAddr: addr, TLS: true}
	assert.Error(t, exportOnce(t, systemRoots), "the test CA is not a system root")
}

func TestTLS_ExplicitConfigWins(t *testing.T) {
	p := newTestPKI(t)
	addr := startTLSCollector(t, p)
	cert, err := tls.LoadX509KeyPair(p.clientCert, p.clientKey)
	require.NoError(t, err)
	cfg := TeleConfig{
		CollectorAddr: addr,
		TLS:           true,
		TLSConfig:     &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: p.caPool, Certificates: []tls.Certificate{cert}},
		CAFile:        "/does/not/exist", // ignored
	}
	assert.NoError(t, exportOnce(t, cfg))
}

func TestTLS_InitWithMutualTLS(t *testing.T) {
	p := newTestPKI(t)
	tele, err := Init(context.Background(), TeleConfig{
		ServiceName:   "svc",
		CollectorAddr: startTLSCollector(t, p),
		TLS:           true,
		CAFile:        p.caFile,
		CertFile:      p.clientCert,
		KeyFile:       p.clientKey,
	})
	require.NoError(t, err)
	ctx := context.Background()
	_, span := tele.TracerProvider.Tracer("t").Start(ctx, "op")
	span.End()
	assert.NoError(t, tele.TracerProvider.ForceFlush(ctx), "spans reach the mTLS collector")
	_ = tele.Shutdown(ctx)
}

func TestClientTLSConfig(t *testing.T) {
	p := newTestPKI(t)
	cfg, err := clientTLSConfig(TeleConfig{CAFile: p.caFile, CertFile: p.clientCert, KeyFile: p.clientKey, ServerName: "collector.test"})
	require.NoError(t, err)
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
	assert.Equal(t, "collector.test", cfg.ServerName)
	assert.True(t, cfg.RootCAs.Equal(p.caPool), "the CA file replaces the system roots")
	assert.Len(t, cfg.Certificates, 1)

	cfg, err = clientTLSConfig(TeleConfig{})
	require.NoError(t, err)
	assert.Nil(t, cfg.RootCAs, "no CA file verifies against the system roots")
	assert.Empty(t, cfg.Certificates)
}

func TestTLS_InvalidSettings(t *testing.T) {
	p := newTestPKI(t)
	notPEM := writeFile(t, t.TempDir(), "bad.pem", []byte("not a certificate"))
	cases := map[string]TeleConfig{
		"CA without TLS":          {CAFile: p.caFile},
		"client cert without TLS": {CertFile: p.clientCert, KeyFile: p.clientKey},
		"ServerName without TLS":  {ServerName: "collector.test"},
		"TLSConfig without TLS":   {TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}},
		"missing CA file":         {TLS: true, CAFile: "/does/not/exist.pem"},
		"CA file without PEM":     {TLS: true, CAFile: notPEM},
		"cert without key":        {TLS: true, CertFile: p.clientCert},
		"key without cert":        {TLS: true, KeyFile: p.clientKey},
		"unreadable key pair":     {TLS: true, CertFile: p.clientCert, KeyFile: notPEM},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			cfg.ServiceName = "svc"
			cfg.CollectorAddr = "127.0.0.1:1"
			tele, err := Init(context.Background(), cfg)
			assert.ErrorIs(t, err, ErrInvalidTLS)
			assert.Nil(t, tele)
		})
	}
}
