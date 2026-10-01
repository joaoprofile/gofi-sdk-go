package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"testing"
	"time"
)

func TestSMTP_SendBulk_LongerThanTimeout(t *testing.T) {
	fs := newFakeServer(t)
	fs.dataDelay = 150 * time.Millisecond
	m, err := New(Config{
		Host: "localhost", Port: fs.port(),
		From:       Address{Email: "f@x.com"},
		Encryption: EncryptionNone,
		MaxRetries: 0, Timeout: 400 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	msgs := make([]*Message, 4) // ~600ms total, above Timeout
	for i := range msgs {
		msgs[i] = sampleMsg(fmt.Sprintf("u%d@x.com", i))
	}
	res, err := m.SendBulk(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("bulk longer than Timeout must not fail mid-batch: %+v", res.Failed)
	}
}

func TestDial_ImplicitTLSIsSeenAsTLS(t *testing.T) {
	ln, err := tls.Listen("tcp", "localhost:0", &tls.Config{Certificates: []tls.Certificate{selfSignedCert(t)}})
	if err != nil {
		t.Fatal(err)
	}
	fs := &fakeServer{ln: ln, badRcpt: map[string]bool{}}
	go fs.serve()
	t.Cleanup(func() { _ = ln.Close() })

	s := &smtpMailer{cfg: Config{
		Host: "localhost", Port: fs.port(), Encryption: EncryptionTLS, Timeout: 2 * time.Second,
		TLSConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed test cert
	}}
	c, err := s.dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, isTLS := c.TLSConnectionState(); !isTLS {
		t.Fatal("implicit TLS connection must be reported as TLS (PLAIN auth depends on it)")
	}
}
