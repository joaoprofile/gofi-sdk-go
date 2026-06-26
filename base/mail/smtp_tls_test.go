package mail

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"

	"github.com/joaoprofile/gofi/base/environment"
)

func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func TestSMTP_STARTTLS(t *testing.T) {
	cert := selfSignedCert(t)
	fs := newFakeServer(t)
	fs.startTLS = &tls.Config{Certificates: []tls.Certificate{cert}}

	m, err := New(Config{
		Host: "localhost", Port: fs.port(),
		From:       Address{Email: "f@x.com"},
		Encryption: EncryptionSTARTTLS,
		Auth:       AuthNone,
		Timeout:    3 * time.Second,
		TLSConfig:  &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed test cert
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := m.Send(context.Background(), sampleMsg("a@b.com")); err != nil {
		t.Fatalf("Send over STARTTLS: %v", err)
	}
	if len(fs.messages()) != 1 {
		t.Fatalf("expected 1 message over STARTTLS, got %d", len(fs.messages()))
	}
}

func TestSMTP_STARTTLS_NotAdvertised(t *testing.T) {
	fs := newFakeServer(t) // startTLS nil → not advertised
	m, _ := New(Config{
		Host: "localhost", Port: fs.port(),
		From: Address{Email: "f@x.com"}, Encryption: EncryptionSTARTTLS, Auth: AuthNone,
		MaxRetries: 0, Timeout: 2 * time.Second,
		TLSConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	})
	if err := m.Send(context.Background(), sampleMsg("a@b.com")); err == nil {
		t.Fatal("expected error when STARTTLS not advertised")
	}
}

func TestSMTP_SendBulk_PoolRecycle(t *testing.T) {
	fs := newFakeServer(t)
	m, err := New(Config{
		Host: "localhost", Port: fs.port(),
		From: Address{Email: "f@x.com"}, Encryption: EncryptionNone, Auth: AuthNone,
		PoolSize: 1, Timeout: 3 * time.Second, // reconnect every message
	})
	if err != nil {
		t.Fatal(err)
	}
	msgs := []*Message{sampleMsg("a@b.com"), sampleMsg("b@b.com"), sampleMsg("c@b.com")}
	res, err := m.SendBulk(context.Background(), msgs)
	if err != nil {
		t.Fatalf("SendBulk: %v", err)
	}
	if res.Sent != 3 || res.HasFailures() {
		t.Fatalf("expected 3 sent, got %+v", res)
	}
	if len(fs.messages()) != 3 {
		t.Fatalf("expected 3 delivered, got %d", len(fs.messages()))
	}
}

func TestNewFromEnv(t *testing.T) {
	t.Setenv("MAIL_HOST", "smtp.example.com")
	t.Setenv("MAIL_FROM_EMAIL", "no-reply@example.com")
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)

	m, err := NewFromEnv()
	if err != nil || m == nil {
		t.Fatalf("NewFromEnv: m=%v err=%v", m, err)
	}
}

func TestNewFromEnv_NotConfigured(t *testing.T) {
	t.Setenv("MAIL_HOST", "")
	t.Setenv("MAIL_FROM_EMAIL", "")
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)

	if _, err := NewFromEnv(); err == nil {
		t.Fatal("expected error when not configured")
	}
}
