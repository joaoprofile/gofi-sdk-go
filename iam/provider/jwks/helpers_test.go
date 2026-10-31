package jwks

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func generateRSAKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	e := priv.E
	eBytes := []byte{byte(e >> 16), byte(e >> 8), byte(e)}
	for len(eBytes) > 1 && eBytes[0] == 0 {
		eBytes = eBytes[1:]
	}
	set := fmt.Sprintf(`{"keys":[{"kty":"RSA","kid":"key1","alg":"RS256","use":"sig","n":"%s","e":"%s"}]}`,
		base64.RawURLEncoding.EncodeToString(priv.N.Bytes()), base64.RawURLEncoding.EncodeToString(eBytes))
	return priv, set
}

// jwksServer serves a key set; status and body can be switched at runtime.
type jwksServer struct {
	*httptest.Server
	status atomic.Int32
	body   atomic.Value
	hits   atomic.Int32
	delay  time.Duration
}

func newJWKSServer(t *testing.T, body string) *jwksServer {
	t.Helper()
	s := &jwksServer{}
	s.status.Store(http.StatusOK)
	s.body.Store(body)
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.hits.Add(1)
		time.Sleep(s.delay)
		w.WriteHeader(int(s.status.Load()))
		_, _ = w.Write([]byte(s.body.Load().(string)))
	}))
	t.Cleanup(s.Close)
	return s
}
