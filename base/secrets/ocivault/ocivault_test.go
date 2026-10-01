package ocivault

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	cloudoci "github.com/gofi-labs/gofi-sdk-go/base/cloud/oci"
	"github.com/gofi-labs/gofi-sdk-go/base/secrets"
)

const secretID = "ocid1.vaultsecret.oc1.sa-saopaulo-1.aaaa"

func pemKey(t *testing.T) string {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)}))
}

func fakeVault(t *testing.T) string {
	t.Helper()
	bundle := func(w http.ResponseWriter, v string) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"secretId": secretID, "versionNumber": 1,
			"secretBundleContent": map[string]string{"contentType": "BASE64", "content": base64.StdEncoding.EncodeToString([]byte(v))},
		})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/20190301/secretbundles/"+secretID:
			bundle(w, `{"password":"pw"}`)
		case r.URL.Path == "/20190301/secretbundles/actions/getByName" && r.URL.Query().Get("secretName") == "db":
			bundle(w, "by-name")
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "NotAuthorizedOrNotFound", "message": "not found"})
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func config(t *testing.T, endpoint string) Config {
	return Config{Endpoint: endpoint, Credentials: cloudoci.Config{
		Region: "sa-saopaulo-1", TenancyID: "ocid1.tenancy.oc1..t", UserID: "ocid1.user.oc1..u",
		Fingerprint: "aa:bb", PrivateKey: pemKey(t),
	}}
}

func TestGet(t *testing.T) {
	s, err := New(config(t, fakeVault(t)))
	if err != nil {
		t.Fatal(err)
	}
	if v, err := s.Get(context.Background(), secretID); err != nil || v != `{"password":"pw"}` {
		t.Errorf("by id: %q,%v", v, err)
	}
	if v, err := s.Get(context.Background(), "ocid1.vault.oc1.sa-saopaulo-1.v/db"); err != nil || v != "by-name" {
		t.Errorf("by name: %q,%v", v, err)
	}
	if _, err := s.Get(context.Background(), "ocid1.vaultsecret.oc1.sa-saopaulo-1.missing"); !errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
}

func TestReference(t *testing.T) {
	Register(config(t, fakeVault(t)))
	v, err := secrets.Resolve(context.Background(), "secret://ocivault/"+secretID+"#password")
	if err != nil || v != "pw" {
		t.Fatalf("got %q,%v", v, err)
	}
}

func TestRegionOf(t *testing.T) {
	if r := regionOf(secretID); r != "sa-saopaulo-1" {
		t.Errorf("regionOf=%q", r)
	}
	if r := regionOf("not-an-ocid"); r != "" {
		t.Errorf("regionOf=%q", r)
	}
}
