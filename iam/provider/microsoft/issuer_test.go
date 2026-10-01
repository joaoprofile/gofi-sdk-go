package microsoft

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
	"strings"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/joaoprofile/gofi-sdk-go/iam/port"
)

const (
	tenantA = "11111111-1111-1111-1111-111111111111"
	tenantB = "22222222-2222-2222-2222-222222222222"
)

// fakeEntra serves discovery, JWKS and token responses in-process for any host.
type fakeEntra struct {
	key     *rsa.PrivateKey
	idToken string
}

func (f *fakeEntra) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	base := "https://" + r.URL.Host
	switch {
	case strings.HasSuffix(r.URL.Path, "/.well-known/openid-configuration"):
		fmt.Fprintf(rec, `{"issuer":"https://login.microsoftonline.com/{tenantid}/v2.0","authorization_endpoint":%q,"token_endpoint":%q,"jwks_uri":%q}`,
			base+"/auth", base+"/token", base+"/keys")
	case r.URL.Path == "/keys":
		n := base64.RawURLEncoding.EncodeToString(f.key.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(f.key.E)).Bytes())
		fmt.Fprintf(rec, `{"keys":[{"kty":"RSA","kid":"k1","n":%q,"e":%q}]}`, n, e)
	case r.URL.Path == "/token":
		fmt.Fprintf(rec, `{"access_token":"at","id_token":%q,"token_type":"Bearer"}`, f.idToken)
	default:
		rec.WriteHeader(http.StatusNotFound)
	}
	return rec.Result(), nil
}

func runCallback(t *testing.T, cfg Config, iss, tid string) error {
	t.Helper()
	claims := gojwt.MapClaims{"iss": iss, "aud": "cid", "sub": "u1", "oid": "o1", "nonce": "n", "exp": time.Now().Add(time.Hour).Unix()}
	if tid != "" {
		claims["tid"] = tid
	}
	_, err := callback(t, cfg, claims)
	return err
}

// callback signs claims as the id_token of a fake Entra and runs the flow.
func callback(t *testing.T, cfg Config, claims gojwt.MapClaims) (*port.IDPCallbackResult, error) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	tok := gojwt.NewWithClaims(gojwt.SigningMethodRS256, claims)
	tok.Header["kid"] = "k1"
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}

	cfg.ClientID = "cid"
	cfg.HTTPClient = &http.Client{Transport: &fakeEntra{key: key, idToken: signed}}
	return New(cfg).HandleCallback(context.Background(), port.IDPCallbackInput{
		State: "s", ExpectedState: "s", ExpectedNonce: "n", Code: "c", CodeVerifier: "v",
	})
}

func issuerFor(tid string) string { return "https://login.microsoftonline.com/" + tid + "/v2.0" }

func TestHandleCallback_Issuer(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		iss     string
		tid     string
		wantErr error
	}{
		{"common accepts org tenant", Config{}, issuerFor(tenantA), tenantA, nil},
		{"common accepts consumer account", Config{}, issuerFor(consumersTenantID), consumersTenantID, nil},
		{"iss must match tid", Config{}, issuerFor(tenantB), tenantA, ErrIssuerMismatch},
		{"missing tid", Config{}, issuerFor(tenantA), "", ErrIssuerMismatch},
		{"foreign issuer host", Config{}, "https://evil.example/" + tenantA + "/v2.0", tenantA, ErrIssuerMismatch},
		{"organizations rejects consumer account", Config{TenantID: "organizations"}, issuerFor(consumersTenantID), consumersTenantID, ErrTenantNotAllowed},
		{"organizations accepts org tenant", Config{TenantID: "organizations"}, issuerFor(tenantA), tenantA, nil},
		{"consumers rejects org tenant", Config{TenantID: "consumers"}, issuerFor(tenantA), tenantA, ErrTenantNotAllowed},
		{"single tenant accepts own tenant", Config{TenantID: tenantA}, issuerFor(tenantA), tenantA, nil},
		{"single tenant rejects other tenant", Config{TenantID: tenantA}, issuerFor(tenantB), tenantB, ErrTenantNotAllowed},
		{"allow-list accepts listed tenant", Config{AllowedTenants: []string{tenantA}}, issuerFor(tenantA), tenantA, nil},
		{"allow-list rejects unlisted tenant", Config{AllowedTenants: []string{tenantA}}, issuerFor(tenantB), tenantB, ErrTenantNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runCallback(t, tt.cfg, tt.iss, tt.tid)
			if tt.wantErr == nil && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("err=%v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestHandleCallback_StableIdentity(t *testing.T) {
	const tid = "72f988bf-86f1-41af-91ab-2d7cd011db47"
	base := func() gojwt.MapClaims {
		return gojwt.MapClaims{"iss": issuerFor(tid), "aud": "cid", "sub": "pairwise", "tid": tid,
			"oid": "OBJ-1", "email": "ceo@victim.example", "email_verified": true,
			"nonce": "n", "exp": time.Now().Add(time.Hour).Unix()}
	}
	res, err := callback(t, Config{}, base())
	if err != nil {
		t.Fatal(err)
	}
	if res.IDPUser.ExternalID != tid+":obj-1" {
		t.Errorf("ExternalID=%q, want tid:oid", res.IDPUser.ExternalID)
	}
	if res.IDPUser.EmailVerified {
		t.Error("Entra e-mail must never be reported as verified")
	}

	noOID := base()
	delete(noOID, "oid")
	if _, err := callback(t, Config{}, noOID); !errors.Is(err, ErrMissingObjectID) {
		t.Errorf("err=%v, want ErrMissingObjectID", err)
	}
}
