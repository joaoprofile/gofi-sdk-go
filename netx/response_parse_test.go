package netx

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/joaoprofile/gofi/base/errs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// JSON

func TestJSON_NilBody_WritesStatusOnly(t *testing.T) {
	rec := httptest.NewRecorder()
	JSON(rec, http.StatusNoContent, nil)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
	assert.Empty(t, rec.Header().Get("Content-Type"))
}

func TestJSON_WithBody_WritesStatusAndBody(t *testing.T) {
	payload := []byte(`{"ok":true}`)
	rec := httptest.NewRecorder()
	JSON(rec, http.StatusOK, payload)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Equal(t, `{"ok":true}`, rec.Body.String())
}

func TestJSON_EmptySlice_WritesBodyAndHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	JSON(rec, http.StatusOK, []byte(`[]`))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "[]", rec.Body.String())
}

// Response

func TestResponse_ValidData_WritesJSONBody(t *testing.T) {
	type payload struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	rec := httptest.NewRecorder()
	Response(rec, http.StatusCreated, payload{ID: 1, Name: "Emilia"})

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var got payload
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, 1, got.ID)
	assert.Equal(t, "Emilia", got.Name)
}

func TestResponse_UnmarshalableData_Returns500(t *testing.T) {
	rec := httptest.NewRecorder()
	// channel cannot be marshalled to JSON
	Response(rec, http.StatusOK, make(chan int))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestResponse_NilData_WritesNull(t *testing.T) {
	rec := httptest.NewRecorder()
	Response(rec, http.StatusOK, nil)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "null", rec.Body.String())
}

// Error

func TestError_WritesStatusAndMessage(t *testing.T) {
	rec := httptest.NewRecorder()
	Error(rec, http.StatusBadRequest, errors.New("invalid input"))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, http.StatusBadRequest, body.StatusCode)
	assert.Equal(t, "invalid input", body.Message)
	assert.Nil(t, body.Details)
}

func TestError_DetailsFieldAbsentWhenNil(t *testing.T) {
	rec := httptest.NewRecorder()
	Error(rec, http.StatusUnprocessableEntity, errors.New("err"))

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	_, hasDetails := body["details"]
	assert.False(t, hasDetails, "details must be omitted when nil")
}

//  ErrorDetails

func TestErrorDetails_WritesMessageAndDetails(t *testing.T) {
	type detail struct {
		Field string `json:"field"`
	}

	rec := httptest.NewRecorder()
	ErrorDetails(rec, http.StatusUnprocessableEntity, "validation failed", detail{Field: "email"})

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	var body ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "validation failed", body.Message)
	assert.NotNil(t, body.Details)
}

func TestErrorDetails_StringDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	ErrorDetails(rec, http.StatusBadRequest, "bad request", "field 'id' is required")

	var body ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "field 'id' is required", body.Details)
}

func TestErrorDetails_SliceDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	ErrorDetails(rec, http.StatusBadRequest, "multiple errors", []string{"err1", "err2"})

	var body struct {
		Details []string `json:"details"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, []string{"err1", "err2"}, body.Details)
}

//  internalError

func TestInternalError_AlwaysReturns500(t *testing.T) {
	rec := httptest.NewRecorder()
	internalError(rec)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, float64(500), body["code"])
	assert.Equal(t, "internal server error", body["message"])
}

// writeError — unmarshalable details trigger internalError fallback

func TestWriteError_UnmarshalableDetails_Returns500(t *testing.T) {
	rec := httptest.NewRecorder()
	// A channel cannot be marshalled to JSON, so json.Marshal(ErrorResponse{...})
	// will fail when Details is a channel, triggering the internalError path.
	ErrorDetails(rec, http.StatusBadRequest, "something failed", make(chan int))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

//  ErrorResponse struct

func TestErrorResponse_OmitsEmptyErrorCode(t *testing.T) {
	er := ErrorResponse{StatusCode: 400, Message: "bad"}
	data, err := json.Marshal(er)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	_, hasErrorCode := m["errorCode"]
	assert.False(t, hasErrorCode, "errorCode must be omitted when empty")
}

func TestErrorResponse_IncludesErrorCodeWhenSet(t *testing.T) {
	er := ErrorResponse{StatusCode: 403, Message: "forbidden", ErrorCode: "AUTH_001"}
	data, err := json.Marshal(er)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	assert.Equal(t, "AUTH_001", m["errorCode"])
}

// RespondError

func TestRespondError_KindMapsToHTTPStatus(t *testing.T) {
	tests := []struct {
		name       string
		appErr     errs.AppError
		wantStatus int
	}{
		{"not_found", errs.RegisterNotFound("rsp-not-found", "resource not found"), http.StatusNotFound},
		{"conflict", errs.RegisterConflict("rsp-conflict", "resource already exists"), http.StatusConflict},
		{"validation", errs.RegisterValidation("rsp-validation", "invalid input"), http.StatusBadRequest},
		{"operation", errs.RegisterOperation("rsp-operation", "operation failed"), http.StatusInternalServerError},
		{"unknown", errs.Register("rsp-unknown", "unknown error"), http.StatusInternalServerError},
		{"external", errs.RegisterExternalError("rsp-external", "external service failed"), http.StatusInternalServerError},
		{"unauthorized", errs.RegisterUnauthorized("rsp-unauthorized", "access denied"), http.StatusInternalServerError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			RespondError(rec, tc.appErr)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		})
	}
}

func TestRespondError_BodyContainsStatusCodeAndMessage(t *testing.T) {
	tests := []struct {
		name       string
		appErr     errs.AppError
		wantStatus int
	}{
		{"not_found", errs.RegisterNotFound("body-nf", "person not found"), http.StatusNotFound},
		{"conflict", errs.RegisterConflict("body-conflict", "email already taken"), http.StatusConflict},
		{"validation", errs.RegisterValidation("body-val", "cpf is required"), http.StatusBadRequest},
		{"operation", errs.RegisterOperation("body-op", "save failed"), http.StatusInternalServerError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			RespondError(rec, tc.appErr)

			var body ErrorResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, tc.wantStatus, body.StatusCode)
			assert.Contains(t, body.Message, tc.appErr.Message)
		})
	}
}

func TestRespondError_WrappedError_PreservesKindAndMessage(t *testing.T) {
	base := errs.RegisterNotFound("wrap-nf", "person %s not found")
	appErr := base.Wrap(errors.New("db: no rows"), "abc-123")

	rec := httptest.NewRecorder()
	RespondError(rec, appErr)

	assert.Equal(t, http.StatusNotFound, rec.Code)

	var body ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, http.StatusNotFound, body.StatusCode)
	assert.Contains(t, body.Message, "abc-123")
}

func TestRespondError_WithDetails_DetailsPropagated(t *testing.T) {
	details := map[string]string{"field": "cpf", "reason": "invalid format"}
	appErr := errs.RegisterValidation("detail-val", "validation failed").
		WithDetails(details)

	rec := httptest.NewRecorder()
	RespondError(rec, appErr)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var body ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Contains(t, body.Message, "validation failed")
	assert.NotNil(t, body.Details)
}
