package jwt

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/iam/types"
)

var (
	oldSecret = []byte(strings.Repeat("a", 32))
	newSecret = []byte(strings.Repeat("b", 32))
)

func issueWith(t *testing.T, cfg Config) string {
	t.Helper()
	p, err := NewProvider(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := p.IssueAccessToken(types.Claims{UserID: "u1", ExpiresAt: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestRotation_OldTokensStayValid(t *testing.T) {
	legacy := issueWith(t, Config{Secret: oldSecret})               // no kid
	withKid := issueWith(t, Config{Secret: oldSecret, KeyID: "k1"}) // kid k1

	rotated, err := NewProvider(Config{Secret: newSecret, KeyID: "k2", VerificationKeys: map[string]any{"k1": oldSecret}})
	if err != nil {
		t.Fatal(err)
	}
	for name, tok := range map[string]string{"legacy": legacy, "kid": withKid, "current": issueWith(t, Config{Secret: newSecret, KeyID: "k2"})} {
		if c, err := rotated.ParseToken(tok); err != nil || c.UserID != "u1" {
			t.Errorf("%s token rejected: %v", name, err)
		}
	}
}

func TestRotation_RetiredKeyIsRejected(t *testing.T) {
	old := issueWith(t, Config{Secret: oldSecret, KeyID: "k1"})
	p, _ := NewProvider(Config{Secret: newSecret, KeyID: "k2"})
	if _, err := p.ParseToken(old); err == nil {
		t.Error("token of a retired key must be rejected")
	}
	forged := issueWith(t, Config{Secret: newSecret, KeyID: "k9"})
	if _, err := p.ParseToken(forged); err == nil {
		t.Error("unknown kid must be rejected")
	}
}

func TestRotation_KidHeaderIsSet(t *testing.T) {
	tok := issueWith(t, Config{Secret: newSecret, KeyID: "k2"})
	p, _ := NewProvider(Config{Secret: newSecret, KeyID: "k2"})
	if _, err := p.ParseToken(tok); err != nil {
		t.Fatal(err)
	}
	header := strings.Split(tok, ".")[0]
	if !strings.Contains(decodeSegment(t, header), `"kid":"k2"`) {
		t.Error("kid header missing")
	}
}

func TestRotation_ShortVerificationSecretRejected(t *testing.T) {
	if _, err := NewProvider(Config{Secret: newSecret, VerificationKeys: map[string]any{"k1": []byte("short")}}); err == nil {
		t.Error("short verification secret must be rejected")
	}
}

func decodeSegment(t *testing.T, seg string) string {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
