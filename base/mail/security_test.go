package mail

import (
	"context"
	"errors"
	"net/smtp"
	"testing"
)

func TestLoginAuth_RefusesCleartextToRemoteHost(t *testing.T) {
	a := &loginAuth{username: "u", password: "p", host: "smtp.x.com"}
	if _, _, err := a.Start(&smtp.ServerInfo{Name: "smtp.x.com", TLS: false}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("plaintext remote: err=%v, want refusal", err)
	}
	if mech, _, err := a.Start(&smtp.ServerInfo{Name: "smtp.x.com", TLS: true}); err != nil || mech != "LOGIN" {
		t.Fatalf("TLS remote: mech=%q err=%v", mech, err)
	}
	if _, _, err := a.Start(&smtp.ServerInfo{Name: "evil.com", TLS: true}); err == nil {
		t.Fatal("wrong host name must be refused")
	}
	for _, h := range []string{"localhost", "127.0.0.1", "::1"} {
		la := &loginAuth{username: "u", password: "p", host: h}
		if _, _, err := la.Start(&smtp.ServerInfo{Name: h}); err != nil {
			t.Errorf("localhost %q: %v", h, err)
		}
	}
}

func TestSMTP_CredentialsWithoutAdvertisedAuthFails(t *testing.T) {
	fs := newFakeServer(t) // requireAuth=false: AUTH is not advertised
	for _, mech := range []AuthMechanism{AuthPlain, AuthLogin, AuthCRAMMD5} {
		m := mailerFor(t, fs, mech)
		if err := m.Send(context.Background(), sampleMsg("a@b.com")); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s: err=%v, want ErrInvalidConfig", mech, err)
		}
	}
	if n := len(fs.messages()); n != 0 {
		t.Fatalf("%d messages relayed without authentication", n)
	}
}

func TestValidate_RejectsAttachmentContentTypeInjection(t *testing.T) {
	m := validMessage()
	m.Attachments = []Attachment{{Filename: "a.txt", ContentType: "text/plain\r\nBcc: spy@evil.com", Content: []byte("x")}}
	if err := m.validate(); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("err=%v, want ErrInvalidHeader", err)
	}
}

func TestValidate_RejectsMalformedAddresses(t *testing.T) {
	for _, bad := range []string{"not-an-email", "Evil <a@b.com>", "a@b.com, c@d.com", "a@", "@b.com"} {
		m := validMessage()
		m.To = []Address{{Email: bad}}
		if err := m.validate(); !errors.Is(err, ErrInvalidHeader) {
			t.Errorf("To %q: err=%v, want ErrInvalidHeader", bad, err)
		}
		m = validMessage()
		m.From.Email = bad
		if err := m.validate(); !errors.Is(err, ErrInvalidHeader) {
			t.Errorf("From %q: err=%v, want ErrInvalidHeader", bad, err)
		}
	}
	if _, err := New(Config{Host: "smtp.x.com", From: Address{Email: "Evil <a@b.com>"}}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("Config.From: err=%v, want ErrInvalidConfig", err)
	}
}
