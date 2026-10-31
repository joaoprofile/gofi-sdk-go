package jwks

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
)

const (
	testIssuer   = "https://customer.example"
	testAudience = "gateway"
)

func newTestVerifier(t *testing.T, srv *jwksServer, mutate func(*Config)) *Verifier {
	t.Helper()
	cfg := Config{JWKSURL: srv.URL, Issuer: testIssuer, Audience: testAudience, HTTPClient: srv.Client()}
	if mutate != nil {
		mutate(&cfg)
	}
	v, err := NewVerifier(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func validClaims() gojwt.MapClaims {
	now := time.Now()
	return gojwt.MapClaims{
		"iss": testIssuer, "aud": testAudience, "sub": "person-1",
		"iat": now.Unix(), "exp": now.Add(10 * time.Minute).Unix(),
		"profile": "EXTERNAL", "account_id": "acc-9",
	}
}

func sign(t *testing.T, method gojwt.SigningMethod, key any, kid string, claims gojwt.MapClaims) string {
	t.Helper()
	tok := gojwt.NewWithClaims(method, claims)
	if kid != "" {
		tok.Header["kid"] = kid
	}
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestVerifier_ValidRS256TokenReturnsClaims(t *testing.T) {
	priv, set := generateRSAKey(t)
	v := newTestVerifier(t, newJWKSServer(t, set), nil)
	claims, err := v.Verify(context.Background(), sign(t, gojwt.SigningMethodRS256, priv, "key1", validClaims()))
	if err != nil {
		t.Fatal(err)
	}
	if claims["sub"] != "person-1" || claims["account_id"] != "acc-9" {
		t.Fatalf("claims=%v", claims)
	}
}

func TestVerifier_ValidES256Token(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := priv.PublicKey.Bytes()
	set := fmt.Sprintf(`{"keys":[{"kty":"EC","kid":"ec1","crv":"P-256","x":"%s","y":"%s"}]}`,
		base64.RawURLEncoding.EncodeToString(raw[1:33]), base64.RawURLEncoding.EncodeToString(raw[33:]))
	v := newTestVerifier(t, newJWKSServer(t, set), nil)
	if _, err := v.Verify(context.Background(), sign(t, gojwt.SigningMethodES256, priv, "ec1", validClaims())); err != nil {
		t.Fatal(err)
	}
}

func TestVerifier_RejectsInvalidTokens(t *testing.T) {
	priv, set := generateRSAKey(t)
	other, _ := generateRSAKey(t)
	srv := newJWKSServer(t, set)
	v := newTestVerifier(t, srv, func(c *Config) { c.MaxLifetime = time.Hour })
	now := time.Now()

	with := func(edit func(gojwt.MapClaims)) gojwt.MapClaims {
		c := validClaims()
		edit(c)
		return c
	}
	cases := map[string]string{
		"wrong issuer":            sign(t, gojwt.SigningMethodRS256, priv, "key1", with(func(c gojwt.MapClaims) { c["iss"] = "https://evil.example" })),
		"wrong audience":          sign(t, gojwt.SigningMethodRS256, priv, "key1", with(func(c gojwt.MapClaims) { c["aud"] = "other" })),
		"expired":                 sign(t, gojwt.SigningMethodRS256, priv, "key1", with(func(c gojwt.MapClaims) { c["exp"] = now.Add(-10 * time.Minute).Unix() })),
		"no exp":                  sign(t, gojwt.SigningMethodRS256, priv, "key1", with(func(c gojwt.MapClaims) { delete(c, "exp") })),
		"no iat with MaxLifetime": sign(t, gojwt.SigningMethodRS256, priv, "key1", with(func(c gojwt.MapClaims) { delete(c, "iat") })),
		"lifetime over max":       sign(t, gojwt.SigningMethodRS256, priv, "key1", with(func(c gojwt.MapClaims) { c["exp"] = now.Add(2 * time.Hour).Unix() })),
		"signed by another key":   sign(t, gojwt.SigningMethodRS256, other, "key1", validClaims()),
		"symmetric algorithm":     sign(t, gojwt.SigningMethodHS256, []byte(strings.Repeat("k", 32)), "key1", validClaims()),
		"unaccepted algorithm":    sign(t, gojwt.SigningMethodRS512, priv, "key1", validClaims()),
		"malformed":               "not-a-jwt",
	}
	for name, tok := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := v.Verify(context.Background(), tok); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("err=%v, want ErrInvalidToken", err)
			}
		})
	}
}

// Attack: the "none" algorithm must never pass, whatever the claims say.
func TestVerifier_RejectsAlgNone(t *testing.T) {
	_, set := generateRSAKey(t)
	v := newTestVerifier(t, newJWKSServer(t, set), nil)
	tok, err := gojwt.NewWithClaims(gojwt.SigningMethodNone, validClaims()).SignedString(gojwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Verify(context.Background(), tok); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err=%v, want ErrInvalidToken", err)
	}
}

// Attack: tokens with random kids must not turn every verification into a JWKS fetch.
func TestVerifier_UnknownKidsDoNotHammerTheIssuer(t *testing.T) {
	priv, set := generateRSAKey(t)
	srv := newJWKSServer(t, set)
	v := newTestVerifier(t, srv, nil)
	for i := range 5 {
		_, _ = v.Verify(context.Background(), sign(t, gojwt.SigningMethodRS256, priv, fmt.Sprint("random-", i), validClaims()))
	}
	if n := srv.hits.Load(); n != 1 {
		t.Fatalf("JWKS fetches=%d, want 1", n)
	}
}

func TestVerifier_CheckFetchesTheKeySet(t *testing.T) {
	_, set := generateRSAKey(t)
	srv := newJWKSServer(t, set)
	n, err := newTestVerifier(t, srv, nil).Check(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestNewVerifier_RejectsUnusableConfig(t *testing.T) {
	base := Config{JWKSURL: "https://customer.example/jwks", Issuer: testIssuer, Audience: testAudience}
	cases := map[string]func(*Config){
		"plain http URL":       func(c *Config) { c.JWKSURL = "http://customer.example/jwks" },
		"missing issuer":       func(c *Config) { c.Issuer = "" },
		"missing audience":     func(c *Config) { c.Audience = "" },
		"symmetric algorithm":  func(c *Config) { c.Algorithms = []string{"HS256"} },
		"none algorithm":       func(c *Config) { c.Algorithms = []string{"none"} },
		"negative MaxLifetime": func(c *Config) { c.MaxLifetime = -time.Second },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := base
			mutate(&cfg)
			if _, err := NewVerifier(cfg); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("err=%v, want ErrInvalidConfig", err)
			}
		})
	}
}

func TestNewVerifier_ClockSkewIsCapped(t *testing.T) {
	v, err := NewVerifier(Config{JWKSURL: "https://customer.example/jwks", Issuer: testIssuer, Audience: testAudience, ClockSkew: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if v.cfg.ClockSkew != 5*time.Minute {
		t.Fatalf("ClockSkew=%s, want the 5m cap", v.cfg.ClockSkew)
	}
}
