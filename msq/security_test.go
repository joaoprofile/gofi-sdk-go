package msq_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/msq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func secretConfig() msq.ProviderConfig {
	return msq.ProviderConfig{
		Type: msq.BrokerKafka, Addr: "k:9092", User: "svc", Password: "s3cret-pw",
		Redis: msq.RedisConnection{Addr: "r:6379", Password: "s3cret-redis"},
		OCI:   msq.OCICredentials{Region: "sa-saopaulo-1", PrivateKey: "-----BEGIN s3cret-key"},
	}
}

func TestProviderConfig_RedactsSecrets(t *testing.T) {
	cfg := secretConfig()
	var logged bytes.Buffer
	slog.New(slog.NewJSONHandler(&logged, nil)).Info("cfg", "cfg", cfg)
	slog.New(slog.NewTextHandler(&logged, nil)).Info("cfg", "cfg", cfg, "redis", cfg.Redis, "oci", cfg.OCI)
	js, err := json.Marshal(cfg)
	require.NoError(t, err)

	outputs := map[string]string{
		"%v":   fmt.Sprintf("%v", cfg),
		"%+v":  fmt.Sprintf("%+v", cfg),
		"%#v":  fmt.Sprintf("%#v", cfg),
		"%s":   fmt.Sprintf("[%s]", cfg),
		"ptr":  fmt.Sprintf("%+v", &cfg),
		"nest": fmt.Sprintf("%+v %#v %+v", cfg.Redis, cfg.Redis, cfg.OCI),
		"json": string(js),
		"slog": logged.String(),
	}
	for name, out := range outputs {
		for _, secret := range []string{"s3cret-pw", "s3cret-redis", "s3cret-key"} {
			assert.NotContains(t, out, secret, "%s leaks a secret: %s", name, out)
		}
		assert.Contains(t, out, "REDACTED", name)
	}
	assert.Contains(t, outputs["%+v"], "k:9092", "non-secret fields stay visible")
	assert.Equal(t, "s3cret-pw", cfg.Password, "redaction must not mutate the config")
}

func TestProviderConfig_Insecure(t *testing.T) {
	cases := []struct {
		name string
		cfg  msq.ProviderConfig
		want bool
	}{
		{"kafka plaintext", msq.ProviderConfig{Type: msq.BrokerKafka}, true},
		{"kafka tls", msq.ProviderConfig{Type: msq.BrokerKafka, UseTLS: true}, false},
		{"tls block implies tls", msq.ProviderConfig{Type: msq.BrokerRabbitMQ, TLS: msq.TLSConfig{CAFile: "ca.pem"}}, false},
		{"skip verify", msq.ProviderConfig{Type: msq.BrokerNATS, UseTLS: true, TLS: msq.TLSConfig{InsecureSkipVerify: true}}, true},
		{"redis plaintext", msq.ProviderConfig{Type: msq.BrokerRedis, UseTLS: true}, true},
		{"redis tls", msq.ProviderConfig{Type: msq.BrokerRedis, Redis: msq.RedisConnection{UseTLS: true}}, false},
		{"sqs", msq.ProviderConfig{Type: msq.BrokerSQS}, false},
		{"oci", msq.ProviderConfig{Type: msq.BrokerOCI}, false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, c.cfg.Insecure(), c.name)
	}
}

func TestTLSConfig_Defaults(t *testing.T) {
	cfg, err := msq.TLSConfig{ServerName: "broker.internal"}.Config()
	require.NoError(t, err)
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
	assert.Equal(t, "broker.internal", cfg.ServerName)
	assert.False(t, cfg.InsecureSkipVerify)
	assert.Nil(t, cfg.RootCAs, "system roots by default")
}

func TestTLSConfig_LoadsCAAndClientCert(t *testing.T) {
	dir := t.TempDir()
	certPEM, keyPEM := selfSigned(t)
	ca, cert, key := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "c.pem"), filepath.Join(dir, "k.pem")
	require.NoError(t, os.WriteFile(ca, certPEM, 0o600))
	require.NoError(t, os.WriteFile(cert, certPEM, 0o600))
	require.NoError(t, os.WriteFile(key, keyPEM, 0o600))

	cfg, err := msq.TLSConfig{CAFile: ca, CertFile: cert, KeyFile: key}.Config()
	require.NoError(t, err)
	assert.NotNil(t, cfg.RootCAs)
	assert.Len(t, cfg.Certificates, 1)

	_, err = msq.TLSConfig{CertFile: cert}.Config()
	assert.ErrorContains(t, err, "both CertFile and KeyFile")
	_, err = msq.TLSConfig{CAFile: key}.Config()
	assert.ErrorContains(t, err, "no PEM certificate")
	_, err = msq.TLSConfig{CAFile: filepath.Join(dir, "missing.pem")}.Config()
	assert.Error(t, err)
}

func selfSigned(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	require.NoError(t, err)
	kder, err := x509.MarshalECPrivateKey(k)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder})
}

func TestReExportsHardeningAPI(t *testing.T) {
	assert.Equal(t, 10, msq.DefaultMaxDeliveries)
	assert.Equal(t, 5*time.Minute, msq.DefaultHandlerTimeout)
	assert.True(t, strings.HasPrefix(msq.HeaderDLQDeliveries, "x-gofi-"))
	assert.NotEqual(t, msq.Ignore, msq.Reject)
}
