package awssm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	cloudaws "github.com/joaoprofile/gofi-sdk-go/base/cloud/aws"
	"github.com/joaoprofile/gofi-sdk-go/base/secrets"
)

// fakeSM answers GetSecretValue like the Secrets Manager JSON API.
func fakeSM(t *testing.T, values map[string]string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ SecretId string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		v, ok := values[in.SecretId]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"__type": "ResourceNotFoundException", "message": "not found"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"Name": in.SecretId, "SecretString": v})
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func newStore(t *testing.T, url string) *Store {
	t.Helper()
	t.Setenv("AWS_CONFIG_FILE", "/nonexistent")
	s, err := New(context.Background(), cloudaws.Config{Region: "us-east-1", Endpoint: url, AccessKeyID: "k", SecretAccessKey: "s"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestGet(t *testing.T) {
	s := newStore(t, fakeSM(t, map[string]string{"prod/db": `{"password":"pw"}`}))
	v, err := s.Get(context.Background(), "prod/db")
	if err != nil || v != `{"password":"pw"}` {
		t.Fatalf("got %q,%v", v, err)
	}
	if _, err := s.Get(context.Background(), "missing"); !errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
}

// A reference resolves through the registered provider and the JSON key.
func TestReference(t *testing.T) {
	url := fakeSM(t, map[string]string{"prod/db": `{"password":"pw"}`})
	t.Setenv("AWS_CONFIG_FILE", "/nonexistent")
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_ACCESS_KEY_ID", "k")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "s")
	t.Setenv("AWS_ENDPOINT_URL_SECRETS_MANAGER", url)
	v, err := secrets.Resolve(context.Background(), "secret://awssm/prod/db#password")
	if err != nil || v != "pw" {
		t.Fatalf("got %q,%v", v, err)
	}
}
