package netx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func statusServer(t *testing.T, statuses ...int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(calls.Add(1)) - 1
		code := statuses[min(i, len(statuses)-1)]
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		w.Write([]byte(`{"message":"x"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestExecute_RetryableGatewayErrors(t *testing.T) {
	for _, code := range []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		srv, calls := statusServer(t, code, http.StatusOK)
		res, err := NewRequest[map[string]string](context.Background(), newTestClient(srv, 1, time.Millisecond), http.MethodGet, "/x").Execute()
		require.NoError(t, err, "status %d", code)
		assert.NotNil(t, res)
		assert.Equal(t, int32(2), calls.Load(), "status %d must be retried", code)
	}
}

func TestExecute_5xxIsNeverSuccess(t *testing.T) {
	for _, code := range []int{http.StatusServiceUnavailable, http.StatusNotImplemented} {
		srv, _ := statusServer(t, code)
		_, err := NewRequest[map[string]string](context.Background(), newTestClient(srv, 0, time.Millisecond), http.MethodGet, "/x").Execute()
		assert.Error(t, err, "status %d must be an error", code)
	}
}

func TestExecute_NotImplementedIsNotRetried(t *testing.T) {
	srv, calls := statusServer(t, http.StatusNotImplemented)
	_, err := NewRequest[map[string]string](context.Background(), newTestClient(srv, 3, time.Millisecond), http.MethodGet, "/x").Execute()
	assert.Error(t, err)
	assert.Equal(t, int32(1), calls.Load())
}

func TestExecute_RetrySleepHonoursContext(t *testing.T) {
	srv, _ := statusServer(t, http.StatusServiceUnavailable)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := NewRequest[map[string]string](ctx, newTestClient(srv, 3, 10*time.Second), http.MethodGet, "/x").Execute()
	assert.Error(t, err)
	assert.Less(t, time.Since(start), 2*time.Second, "retry sleep must stop when ctx is done")
}

func TestExecute_PostWithoutIdempotencyKeyIsNotRetried(t *testing.T) {
	srv, calls := statusServer(t, http.StatusServiceUnavailable, http.StatusOK)
	_, err := NewRequest[map[string]string](context.Background(), newTestClient(srv, 3, time.Millisecond), http.MethodPost, "/x").Execute()
	assert.Error(t, err)
	assert.Equal(t, int32(1), calls.Load(), "a POST could duplicate side effects")
}

func TestExecute_PostWithIdempotencyKeyIsRetried(t *testing.T) {
	srv, calls := statusServer(t, http.StatusServiceUnavailable, http.StatusOK)
	req := NewRequest[map[string]string](context.Background(), newTestClient(srv, 3, time.Millisecond), http.MethodPost, "/x")
	req.SetHeader("Idempotency-Key", "k-1")
	_, err := req.Execute()
	assert.NoError(t, err)
	assert.Equal(t, int32(2), calls.Load())
}

func TestExecute_429IsRetriedForAnyMethod(t *testing.T) {
	srv, calls := statusServer(t, http.StatusTooManyRequests, http.StatusOK)
	_, err := NewRequest[map[string]string](context.Background(), newTestClient(srv, 3, time.Millisecond), http.MethodPost, "/x").Execute()
	assert.NoError(t, err)
	assert.Equal(t, int32(2), calls.Load())
}
