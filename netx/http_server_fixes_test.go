package netx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofi-labs/gofi-sdk-go/base/errs"
	"github.com/stretchr/testify/assert"
)

func TestGetParams_PreserveCase(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/x?token=AbC123", nil)
	r.SetPathValue("id", "Ord-9XZ")
	assert.Equal(t, "AbC123", GetQueryParam("token", r))
	assert.Equal(t, "Ord-9XZ", GetPathParam("id", r))
}

func TestResponseWriter_SupportsFlush(t *testing.T) {
	var w http.ResponseWriter = &responseWriter{ResponseWriter: httptest.NewRecorder(), statusCode: 200}
	f, ok := w.(http.Flusher)
	assert.True(t, ok, "SSE handlers assert http.Flusher")
	f.Flush()
}

func TestRespondError_ForbiddenAndExternalStatuses(t *testing.T) {
	cases := map[errs.AppError]int{
		errs.RegisterForbidden("fx-forbidden", "no"):          http.StatusForbidden,
		errs.RegisterExternalError("fx-upstream", "upstream"): http.StatusBadGateway,
	}
	for e, want := range cases {
		rec := httptest.NewRecorder()
		RespondError(rec, nil, e)
		assert.Equal(t, want, rec.Code, e.Code)
	}
}
