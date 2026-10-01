package netx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type headerSigner struct{}

func (headerSigner) Sign(r *http.Request, body []byte) (*http.Request, error) {
	r.Header.Set("X-Signed", string(body))
	return r, nil
}

func TestRequest_SignatureSeesBody(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Signed")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	req := &Request[struct{}]{Ctx: t.Context(), HttpMethod: http.MethodPost, Url: srv.URL, Body: map[string]int{"a": 1}}
	req.SetSignature(headerSigner{})
	hr, err := req.createHttpRequest(req.prepareRequestBody())
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(hr)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, `{"a":1}`, got)
}
