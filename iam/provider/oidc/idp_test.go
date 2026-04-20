package oidc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/joaoprofile/gofi/iam/core"
	"github.com/joaoprofile/gofi/iam/port"
)

// ---- helpers ----

func buildDiscoveryServer(t *testing.T, jwksPath string) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"issuer": "%s",
			"authorization_endpoint": "%s/auth",
			"token_endpoint": "%s/token",
			"userinfo_endpoint": "%s/userinfo",
			"jwks_uri": "%s%s"
		}`, "placeholder", "placeholder", "placeholder", "placeholder", "placeholder", jwksPath)
	}))
	return ts
}

func buildProvider(t *testing.T, issuerURL string, httpClient *http.Client) *Provider {
	t.Helper()
	return New("test", Config{
		IssuerURL:    issuerURL,
		ClientID:     "client1",
		ClientSecret: "secret1",
		RedirectURI:  "http://localhost/callback",
		HTTPClient:   httpClient,
	})
}

// rsaJWKS returns an RSA public key JWK set JSON and the corresponding signing key.
func generateRSAKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	nB64 := base64.RawURLEncoding.EncodeToString(priv.N.Bytes())
	e := priv.E
	eBytes := []byte{byte(e >> 16), byte(e >> 8), byte(e)}
	// Trim leading zeros
	for len(eBytes) > 1 && eBytes[0] == 0 {
		eBytes = eBytes[1:]
	}
	eB64 := base64.RawURLEncoding.EncodeToString(eBytes)

	jwks := fmt.Sprintf(`{"keys":[{"kty":"RSA","kid":"key1","alg":"RS256","use":"sig","n":"%s","e":"%s"}]}`, nB64, eB64)
	return priv, jwks
}

func generateECKey(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	xB64 := base64.RawURLEncoding.EncodeToString(priv.X.Bytes())
	yB64 := base64.RawURLEncoding.EncodeToString(priv.Y.Bytes())

	jwks := fmt.Sprintf(`{"keys":[{"kty":"EC","kid":"eckey1","alg":"ES256","crv":"P-256","x":"%s","y":"%s"}]}`, xB64, yB64)
	return priv, jwks
}

// buildFullServer returns a test HTTP server that answers discovery, JWKS, and token endpoints.
func buildFullServer(t *testing.T, privKey *rsa.PrivateKey, jwksJSON string, idToken string) *httptest.Server {
	t.Helper()
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			fmt.Fprintf(w, `{
				"issuer": "%s",
				"authorization_endpoint": "%s/auth",
				"token_endpoint": "%s/token",
				"userinfo_endpoint": "%s/userinfo",
				"jwks_uri": "%s/jwks"
			}`, ts.URL, ts.URL, ts.URL, ts.URL, ts.URL)
		case "/jwks":
			w.Write([]byte(jwksJSON))
		case "/token":
			fmt.Fprintf(w, `{"access_token":"at","id_token":"%s","token_type":"Bearer","expires_in":3600}`, idToken)
		default:
			http.NotFound(w, r)
		}
	}))
	return ts
}

func signRSAToken(t *testing.T, key *rsa.PrivateKey, issuer, audience, kid string) string {
	t.Helper()
	token := gojwt.NewWithClaims(gojwt.SigningMethodRS256, gojwt.MapClaims{
		"iss":            issuer,
		"aud":            audience,
		"sub":            "user123",
		"email":          "user@example.com",
		"email_verified": true,
		"name":           "Test User",
		"picture":        "https://example.com/pic.jpg",
		"iat":            time.Now().Unix(),
		"exp":            time.Now().Add(time.Hour).Unix(),
	})
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}
	return signed
}

// ---- New / ProviderName ----

func TestNew_Defaults(t *testing.T) {
	p := New("myoidc", Config{
		IssuerURL: "https://example.com",
		ClientID:  "cid",
	})
	if p.ProviderName() != "myoidc" {
		t.Errorf("ProviderName=%q, want myoidc", p.ProviderName())
	}
	if p.cfg.JWKSCacheTTL != time.Hour {
		t.Errorf("JWKSCacheTTL=%v, want 1h", p.cfg.JWKSCacheTTL)
	}
	if p.client == nil {
		t.Error("expected non-nil http.Client")
	}
}

func TestNew_WithCustomHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 5 * time.Second}
	p := New("p", Config{HTTPClient: custom})
	if p.client != custom {
		t.Error("expected custom HTTP client to be used")
	}
}

// ---- unique ----

func TestUnique(t *testing.T) {
	in := []string{"a", "b", "a", "c", "b", "d"}
	out := unique(in)
	if len(out) != 4 {
		t.Errorf("unique len=%d, want 4", len(out))
	}
	// Preserve first occurrence.
	if out[0] != "a" || out[1] != "b" || out[2] != "c" || out[3] != "d" {
		t.Errorf("unexpected order: %v", out)
	}
}

func TestUnique_Empty(t *testing.T) {
	out := unique(nil)
	if len(out) != 0 {
		t.Errorf("expected empty, got %v", out)
	}
}

// ---- generatePKCE ----

func TestGeneratePKCE_Success(t *testing.T) {
	verifier, challenge, err := generatePKCE()
	if err != nil {
		t.Fatalf("generatePKCE() error: %v", err)
	}
	if verifier == "" {
		t.Error("expected non-empty verifier")
	}
	if challenge == "" {
		t.Error("expected non-empty challenge")
	}
	if verifier == challenge {
		t.Error("verifier and challenge should differ")
	}
}

func TestGeneratePKCE_RandReadError(t *testing.T) {
	original := cryptoRandRead
	t.Cleanup(func() { cryptoRandRead = original })

	cryptoRandRead = func(b []byte) (int, error) {
		return 0, errors.New("rand error")
	}

	_, _, err := generatePKCE()
	if err == nil {
		t.Fatal("expected error when rand.Read fails")
	}
}

// ---- mapClaims ----

func TestMapClaims_AllFields(t *testing.T) {
	mc := gojwt.MapClaims{
		"sub":            "u1",
		"email":          "u1@example.com",
		"email_verified": true,
		"name":           "User One",
		"picture":        "https://example.com/pic.jpg",
		"custom":         "value",
	}

	u := mapClaims(mc, "google")
	if u.ExternalID != "u1" {
		t.Errorf("ExternalID=%q, want u1", u.ExternalID)
	}
	if u.Provider != "google" {
		t.Errorf("Provider=%q, want google", u.Provider)
	}
	if !u.EmailVerified {
		t.Error("expected EmailVerified=true")
	}
	if u.RawClaims["custom"] != "value" {
		t.Errorf("RawClaims[custom]=%v, want value", u.RawClaims["custom"])
	}
}

func TestMapClaims_MissingOptionalFields(t *testing.T) {
	mc := gojwt.MapClaims{"sub": "u2"}
	u := mapClaims(mc, "oidc")
	if u.ExternalID != "u2" {
		t.Errorf("ExternalID=%q, want u2", u.ExternalID)
	}
	if u.Email != "" {
		t.Errorf("Email should be empty, got %q", u.Email)
	}
}

// ---- jwkKey.PublicKey ----

func TestJWKKey_RSAPublicKey(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	e := priv.E
	eBytes := []byte{byte(e >> 16), byte(e >> 8), byte(e)}
	for len(eBytes) > 1 && eBytes[0] == 0 {
		eBytes = eBytes[1:]
	}

	k := &jwkKey{
		Kty: "RSA",
		N:   base64.RawURLEncoding.EncodeToString(priv.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(eBytes),
	}

	pub, err := k.PublicKey()
	if err != nil {
		t.Fatalf("PublicKey() error: %v", err)
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		t.Fatal("expected *rsa.PublicKey")
	}
	if rsaPub.N.Cmp(priv.N) != 0 {
		t.Error("N mismatch")
	}
}

func TestJWKKey_RSAPublicKey_InvalidN(t *testing.T) {
	k := &jwkKey{Kty: "RSA", N: "!invalid!", E: "AQAB"}
	_, err := k.PublicKey()
	if err == nil {
		t.Fatal("expected error for invalid N")
	}
}

func TestJWKKey_RSAPublicKey_InvalidE(t *testing.T) {
	k := &jwkKey{Kty: "RSA", N: base64.RawURLEncoding.EncodeToString(big.NewInt(42).Bytes()), E: "!invalid!"}
	_, err := k.PublicKey()
	if err == nil {
		t.Fatal("expected error for invalid E")
	}
}

func TestJWKKey_ECPublicKey_P256(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	k := &jwkKey{
		Kty: "EC",
		Crv: "P-256",
		X:   base64.RawURLEncoding.EncodeToString(priv.X.Bytes()),
		Y:   base64.RawURLEncoding.EncodeToString(priv.Y.Bytes()),
	}

	pub, err := k.PublicKey()
	if err != nil {
		t.Fatalf("PublicKey() error: %v", err)
	}
	if _, ok := pub.(*ecdsa.PublicKey); !ok {
		t.Fatal("expected *ecdsa.PublicKey")
	}
}

func TestJWKKey_ECPublicKey_P384(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	k := &jwkKey{
		Kty: "EC",
		Crv: "P-384",
		X:   base64.RawURLEncoding.EncodeToString(priv.X.Bytes()),
		Y:   base64.RawURLEncoding.EncodeToString(priv.Y.Bytes()),
	}

	_, err = k.PublicKey()
	if err != nil {
		t.Fatalf("PublicKey() error: %v", err)
	}
}

func TestJWKKey_ECPublicKey_P521(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	k := &jwkKey{
		Kty: "EC",
		Crv: "P-521",
		X:   base64.RawURLEncoding.EncodeToString(priv.X.Bytes()),
		Y:   base64.RawURLEncoding.EncodeToString(priv.Y.Bytes()),
	}

	_, err = k.PublicKey()
	if err != nil {
		t.Fatalf("PublicKey() error: %v", err)
	}
}

func TestJWKKey_ECPublicKey_InvalidX(t *testing.T) {
	k := &jwkKey{Kty: "EC", Crv: "P-256", X: "!invalid!", Y: "AAAA"}
	_, err := k.PublicKey()
	if err == nil {
		t.Fatal("expected error for invalid X")
	}
}

func TestJWKKey_ECPublicKey_InvalidY(t *testing.T) {
	k := &jwkKey{
		Kty: "EC",
		Crv: "P-256",
		X:   base64.RawURLEncoding.EncodeToString(big.NewInt(1).Bytes()),
		Y:   "!invalid!",
	}
	_, err := k.PublicKey()
	if err == nil {
		t.Fatal("expected error for invalid Y")
	}
}

func TestJWKKey_ECPublicKey_UnsupportedCurve(t *testing.T) {
	k := &jwkKey{
		Kty: "EC",
		Crv: "P-999",
		X:   base64.RawURLEncoding.EncodeToString(big.NewInt(1).Bytes()),
		Y:   base64.RawURLEncoding.EncodeToString(big.NewInt(1).Bytes()),
	}
	_, err := k.PublicKey()
	if err == nil {
		t.Fatal("expected error for unsupported curve")
	}
}

func TestJWKKey_UnsupportedKeyType(t *testing.T) {
	k := &jwkKey{Kty: "oct"}
	_, err := k.PublicKey()
	if err == nil {
		t.Fatal("expected error for unsupported key type")
	}
}

// ---- elliptic curve functions ----

func TestEllipticCurveFunctions(t *testing.T) {
	if ellipticP256() == nil {
		t.Error("ellipticP256() returned nil")
	}
	if ellipticP384() == nil {
		t.Error("ellipticP384() returned nil")
	}
	if ellipticP521() == nil {
		t.Error("ellipticP521() returned nil")
	}
}

// ---- getDiscovery ----

func TestGetDiscovery_NetworkError(t *testing.T) {
	p := buildProvider(t, "http://127.0.0.1:1", nil)
	_, err := p.getDiscovery(context.Background())
	if err == nil {
		t.Fatal("expected error for unreachable host")
	}
}

func TestGetDiscovery_InvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not-json"))
	}))
	defer ts.Close()

	p := buildProvider(t, ts.URL, ts.Client())
	_, err := p.getDiscovery(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid discovery JSON")
	}
}

func TestGetDiscovery_CachedAfterFirstCall(t *testing.T) {
	callCount := 0
	var tsURL string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"issuer":"%s","authorization_endpoint":"%s/auth","token_endpoint":"%s/token","jwks_uri":"%s/jwks"}`,
			tsURL, tsURL, tsURL, tsURL)
	}))
	tsURL = ts.URL
	defer ts.Close()

	p := buildProvider(t, ts.URL, ts.Client())
	ctx := context.Background()

	if _, err := p.getDiscovery(ctx); err != nil {
		t.Fatalf("first discovery error: %v", err)
	}
	if _, err := p.getDiscovery(ctx); err != nil {
		t.Fatalf("second discovery error: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 discovery HTTP call, got %d", callCount)
	}
}

// ---- AuthorizationURL ----

func TestAuthorizationURL_Success(t *testing.T) {
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"issuer":"%s","authorization_endpoint":"%s/auth","token_endpoint":"%s/token","jwks_uri":"%s/jwks"}`,
			ts.URL, ts.URL, ts.URL, ts.URL)
	}))
	defer ts.Close()

	p := buildProvider(t, ts.URL, ts.Client())
	input := port.IDPAuthInput{
		State:       "state123",
		Nonce:       "nonce456",
		RedirectURI: "http://localhost/callback",
		Scopes:      []string{"extra"},
	}

	result, err := p.AuthorizationURL(context.Background(), input)
	if err != nil {
		t.Fatalf("AuthorizationURL() error: %v", err)
	}
	if result.URL == "" {
		t.Error("expected non-empty URL")
	}
	if result.CodeVerifier == "" {
		t.Error("expected non-empty CodeVerifier")
	}
	if result.State != "state123" {
		t.Errorf("State=%q, want state123", result.State)
	}
}

func TestAuthorizationURL_DiscoveryError(t *testing.T) {
	p := buildProvider(t, "http://127.0.0.1:1", nil)
	_, err := p.AuthorizationURL(context.Background(), port.IDPAuthInput{})
	if err == nil {
		t.Fatal("expected error when discovery fails")
	}
}

func TestAuthorizationURL_PKCEError(t *testing.T) {
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"issuer":"%s","authorization_endpoint":"%s/auth","token_endpoint":"%s/token","jwks_uri":"%s/jwks"}`,
			ts.URL, ts.URL, ts.URL, ts.URL)
	}))
	defer ts.Close()

	original := cryptoRandRead
	t.Cleanup(func() { cryptoRandRead = original })
	cryptoRandRead = func(b []byte) (int, error) { return 0, errors.New("rand error") }

	p := buildProvider(t, ts.URL, ts.Client())
	_, err := p.AuthorizationURL(context.Background(), port.IDPAuthInput{})
	if err == nil {
		t.Fatal("expected error when PKCE generation fails")
	}
}

// ---- HandleCallback ----

func TestHandleCallback_StateMismatch(t *testing.T) {
	p := buildProvider(t, "https://example.com", nil)
	_, err := p.HandleCallback(context.Background(), port.IDPCallbackInput{
		State:         "wrong",
		ExpectedState: "correct",
	})
	if err == nil {
		t.Fatal("expected error for state mismatch")
	}
	if !errors.Is(err, core.ErrInvalidIDPState) {
		t.Errorf("expected ErrInvalidIDPState, got %v", err)
	}
}

func TestHandleCallback_DiscoveryError(t *testing.T) {
	p := buildProvider(t, "http://127.0.0.1:1", nil)
	_, err := p.HandleCallback(context.Background(), port.IDPCallbackInput{
		State:         "s",
		ExpectedState: "s",
	})
	if err == nil {
		t.Fatal("expected error when discovery fails")
	}
}

func TestHandleCallback_TokenExchangeError(t *testing.T) {
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			fmt.Fprintf(w, `{"issuer":"%s","authorization_endpoint":"%s/auth","token_endpoint":"%s/token","jwks_uri":"%s/jwks"}`,
				ts.URL, ts.URL, ts.URL, ts.URL)
		case "/token":
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid_grant"}`))
		}
	}))
	defer ts.Close()

	p := buildProvider(t, ts.URL, ts.Client())
	_, err := p.HandleCallback(context.Background(), port.IDPCallbackInput{
		State:         "s",
		ExpectedState: "s",
		Code:          "code123",
	})
	if err == nil {
		t.Fatal("expected token exchange error")
	}
}

func TestHandleCallback_FullFlow_RSA(t *testing.T) {
	rsaKey, jwksJSON := generateRSAKey(t)

	var ts *httptest.Server
	// The id_token issuer must match the server URL — build server first, then sign.
	// We use a two-pass approach: sign after server is running.
	var idToken string
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			fmt.Fprintf(w, `{"issuer":"%s","authorization_endpoint":"%s/auth","token_endpoint":"%s/token","jwks_uri":"%s/jwks"}`,
				ts.URL, ts.URL, ts.URL, ts.URL)
		case "/jwks":
			w.Write([]byte(jwksJSON))
		case "/token":
			fmt.Fprintf(w, `{"access_token":"at","id_token":"%s","token_type":"Bearer","expires_in":3600}`, idToken)
		}
	}))
	defer ts.Close()

	// Sign the token with the server URL as issuer.
	idToken = signRSAToken(t, rsaKey, ts.URL, "client1", "key1")

	p := buildProvider(t, ts.URL, ts.Client())
	result, err := p.HandleCallback(context.Background(), port.IDPCallbackInput{
		State:         "s",
		ExpectedState: "s",
		Code:          "code123",
		RedirectURI:   "http://localhost/callback",
		CodeVerifier:  "verifier",
	})
	if err != nil {
		t.Fatalf("HandleCallback() error: %v", err)
	}
	if result.IDPUser.ExternalID != "user123" {
		t.Errorf("ExternalID=%q, want user123", result.IDPUser.ExternalID)
	}
}

// ---- jwksCache ----

func TestJWKSCache_KeyNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"keys":[]}`))
	}))
	defer ts.Close()

	c := &jwksCache{ttl: time.Hour}
	_, err := c.GetKey(context.Background(), ts.Client(), ts.URL, "missingkid")
	if err == nil {
		t.Fatal("expected error for missing kid")
	}
}

func TestJWKSCache_FetchNetworkError(t *testing.T) {
	c := &jwksCache{ttl: time.Hour}
	_, err := c.GetKey(context.Background(), &http.Client{}, "http://127.0.0.1:1", "kid")
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestJWKSCache_FetchInvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not-json"))
	}))
	defer ts.Close()

	c := &jwksCache{ttl: time.Hour}
	_, err := c.GetKey(context.Background(), ts.Client(), ts.URL, "kid")
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestJWKSCache_CacheHit(t *testing.T) {
	callCount := 0
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	e := priv.E
	eBytes := []byte{byte(e >> 16), byte(e >> 8), byte(e)}
	for len(eBytes) > 1 && eBytes[0] == 0 {
		eBytes = eBytes[1:]
	}
	jwks := fmt.Sprintf(`{"keys":[{"kty":"RSA","kid":"k1","n":"%s","e":"%s"}]}`,
		base64.RawURLEncoding.EncodeToString(priv.N.Bytes()),
		base64.RawURLEncoding.EncodeToString(eBytes))

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Write([]byte(jwks))
	}))
	defer ts.Close()

	c := &jwksCache{ttl: time.Hour}
	ctx := context.Background()

	if _, err := c.GetKey(ctx, ts.Client(), ts.URL, "k1"); err != nil {
		t.Fatalf("first GetKey error: %v", err)
	}
	if _, err := c.GetKey(ctx, ts.Client(), ts.URL, "k1"); err != nil {
		t.Fatalf("second GetKey error: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 JWKS fetch, got %d", callCount)
	}
}

func TestJWKSCache_SkipsMalformedKeys(t *testing.T) {
	// A JWKS with an unknown key type that gets skipped.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"keys":[{"kty":"oct","kid":"k1"}]}`))
	}))
	defer ts.Close()

	c := &jwksCache{ttl: time.Hour}
	_, err := c.GetKey(context.Background(), ts.Client(), ts.URL, "k1")
	if err == nil {
		t.Fatal("expected key not found error after skipping malformed key")
	}
}

// ---- exchangeCode ----

func TestExchangeCode_NetworkError(t *testing.T) {
	p := buildProvider(t, "https://example.com", &http.Client{})
	_, err := p.exchangeCode(context.Background(), "http://127.0.0.1:1/token", port.IDPCallbackInput{})
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestExchangeCode_InvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not-json"))
	}))
	defer ts.Close()

	p := buildProvider(t, "https://example.com", ts.Client())
	_, err := p.exchangeCode(context.Background(), ts.URL, port.IDPCallbackInput{})
	if err == nil {
		t.Fatal("expected JSON decode error")
	}
}

// ---- validateIDToken ----

func TestValidateIDToken_InvalidJWT(t *testing.T) {
	p := buildProvider(t, "https://example.com", nil)
	_, err := p.validateIDToken(context.Background(), &discoveryDoc{}, "not.a.jwt")
	if err == nil {
		t.Fatal("expected error for invalid JWT")
	}
}

func TestValidateIDToken_ES256(t *testing.T) {
	ecKey, jwksJSON := generateECKey(t)

	var ts *httptest.Server
	var idToken string
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/jwks":
			w.Write([]byte(jwksJSON))
		}
	}))
	defer ts.Close()

	// Sign with ES256.
	token := gojwt.NewWithClaims(gojwt.SigningMethodES256, gojwt.MapClaims{
		"iss": ts.URL,
		"aud": "client1",
		"sub": "ecuser",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	})
	token.Header["kid"] = "eckey1"
	signed, err := token.SignedString(ecKey)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	idToken = signed

	p := New("test", Config{
		IssuerURL:  ts.URL,
		ClientID:   "client1",
		HTTPClient: ts.Client(),
	})
	disc := &discoveryDoc{JWKSURI: ts.URL + "/jwks"}

	result, err := p.validateIDToken(context.Background(), disc, idToken)
	if err != nil {
		t.Fatalf("validateIDToken() error: %v", err)
	}
	if result.ExternalID != "ecuser" {
		t.Errorf("ExternalID=%q, want ecuser", result.ExternalID)
	}
}

// serialises JWKS and returns the JSON bytes
func marshalJWKS(keys []map[string]any) []byte {
	b, _ := json.Marshal(map[string]any{"keys": keys})
	return b
}
