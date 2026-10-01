package oci

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveNamespace_RetriesAfterFailure(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"code":"InvalidParameter","message":"transient"}`))
			return
		}
		w.Write([]byte(`"tenancy-ns"`))
	}))
	defer ts.Close()

	s, err := New(validConfig())
	require.NoError(t, err)
	s.client.Host = ts.URL
	s.client.HTTPClient = ts.Client()

	_, err = s.resolveNamespace(context.Background())
	require.Error(t, err)

	ns, err := s.resolveNamespace(context.Background())
	require.NoError(t, err, "a failed lookup must not be cached")
	assert.Equal(t, "tenancy-ns", ns)

	_, _ = s.resolveNamespace(context.Background())
	assert.Equal(t, int32(2), calls.Load(), "success must be cached")
}

func TestResolveNamespace_IgnoresCallerCancellation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`"tenancy-ns"`))
	}))
	defer ts.Close()

	s, err := New(validConfig())
	require.NoError(t, err)
	s.client.Host = ts.URL
	s.client.HTTPClient = ts.Client()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ns, err := s.resolveNamespace(ctx)
	require.NoError(t, err)
	assert.Equal(t, "tenancy-ns", ns)
}
