package google

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/iam/port"
	gojwt "github.com/golang-jwt/jwt/v5"
)

// fakeGoogle serves discovery, JWKS and token responses in-process for any host.
type fakeGoogle struct {
	key     *rsa.PrivateKey
	idToken string
}

func newFakeGoogle(t *testing.T) *fakeGoogle {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return &fakeGoogle{key: key}
}

func (f *fakeGoogle) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		fmt.Fprintf(rec, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"jwks_uri":%q}`,
			issuerURL, issuerURL+"/auth", issuerURL+"/token", issuerURL+"/jwks")
	case "/jwks":
		n := base64.RawURLEncoding.EncodeToString(f.key.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(f.key.E)).Bytes())
		fmt.Fprintf(rec, `{"keys":[{"kty":"RSA","kid":"k1","alg":"RS256","n":%q,"e":%q}]}`, n, e)
	case "/token":
		fmt.Fprintf(rec, `{"access_token":"at","id_token":%q,"token_type":"Bearer"}`, f.idToken)
	default:
		rec.WriteHeader(http.StatusNotFound)
	}
	return rec.Result(), nil
}

func (f *fakeGoogle) sign(t *testing.T, hd string) {
	t.Helper()
	claims := gojwt.MapClaims{
		"iss":   issuerURL,
		"aud":   "cid",
		"sub":   "u1",
		"email": "u1@example.com",
		"nonce": "n",
		"exp":   time.Now().Add(time.Hour).Unix(),
	}
	if hd != "" {
		claims["hd"] = hd
	}
	tok := gojwt.NewWithClaims(gojwt.SigningMethodRS256, claims)
	tok.Header["kid"] = "k1"
	s, err := tok.SignedString(f.key)
	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}
	f.idToken = s
}

func callback(p *Provider) error {
	_, err := p.HandleCallback(context.Background(), port.IDPCallbackInput{
		State: "s", ExpectedState: "s", ExpectedNonce: "n", Code: "c", RedirectURI: "http://localhost/cb", CodeVerifier: "v",
	})
	return err
}

func TestHandleCallback_HostedDomain(t *testing.T) {
	tests := []struct {
		name    string
		tokenHD string
		wantErr bool
	}{
		{"matching domain", "example.com", false},
		{"matching domain case-insensitive", "Example.COM", false},
		{"other workspace domain", "evil.com", true},
		{"consumer account without hd", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fg := newFakeGoogle(t)
			fg.sign(t, tt.tokenHD)
			p := New(Config{ClientID: "cid", HostedDomain: "example.com", HTTPClient: &http.Client{Transport: fg}})

			err := callback(p)
			if tt.wantErr != (err != nil) {
				t.Fatalf("err=%v, wantErr=%v", err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, ErrHostedDomainMismatch) {
				t.Fatalf("err=%v, want ErrHostedDomainMismatch", err)
			}
		})
	}
}

func TestHandleCallback_NoHostedDomainAcceptsAnyAccount(t *testing.T) {
	fg := newFakeGoogle(t)
	fg.sign(t, "")
	p := New(Config{ClientID: "cid", HTTPClient: &http.Client{Transport: fg}})

	if err := callback(p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAuthorizationURL_SendsHostedDomainParam(t *testing.T) {
	p := New(Config{ClientID: "cid", HostedDomain: "example.com", HTTPClient: &http.Client{Transport: newFakeGoogle(t)}})

	res, err := p.AuthorizationURL(context.Background(), port.IDPAuthInput{State: "s", Nonce: "n", RedirectURI: "http://localhost/cb"})
	if err != nil {
		t.Fatalf("AuthorizationURL: %v", err)
	}
	u, _ := url.Parse(res.URL)
	q := u.Query()
	if got := q.Get("hd"); got != "example.com" {
		t.Errorf("hd param=%q, want example.com", got)
	}
	if strings.Contains(" "+q.Get("scope")+" ", " hd ") {
		t.Errorf("scope must not contain hd: %q", q.Get("scope"))
	}
}
