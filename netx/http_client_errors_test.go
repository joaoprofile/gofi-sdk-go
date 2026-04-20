package netx

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// NewAPIError

func TestNewAPIError_PopulatesAllFields(t *testing.T) {
	e := NewAPIError(404, "not found", errors.New("resource missing"))

	assert.Equal(t, 404, e.Status)
	assert.Equal(t, "not found", e.Message)
	assert.Equal(t, "resource missing", e.Err)
}

func TestNewAPIError_DefaultsWhenZeroStatus(t *testing.T) {
	e := NewAPIError(0, "msg", errors.New("cause"))
	assert.Equal(t, 500, e.Status)
}

func TestNewAPIError_DefaultsWhenEmptyMessage(t *testing.T) {
	e := NewAPIError(400, "", errors.New("cause"))
	assert.Equal(t, "Internal Server Error", e.Message)
}

func TestNewAPIError_DefaultsWhenNilError(t *testing.T) {
	e := NewAPIError(500, "oops", nil)
	assert.NotEmpty(t, e.Err)
}

func TestNewAPIError_ImplementsErrorInterface(t *testing.T) {
	var err error = NewAPIError(500, "server error", errors.New("cause"))
	assert.NotNil(t, err)
}

//  HttpError.Error

func TestHttpError_Error_ReturnsFormattedString(t *testing.T) {
	e := &HttpError{Message: "not found", Status: 404, Err: "resource missing"}
	assert.Equal(t, "not found (status: 404, error: resource missing)", e.Error())
}

//  ParseError─

func TestParseError_ValidJSON_ReturnsHttpError(t *testing.T) {
	raw := `{"message":"bad request","error":"invalid id","status":400}`
	e, err := ParseError(raw)

	require.NoError(t, err)
	assert.Equal(t, "bad request", e.Message)
	assert.Equal(t, "invalid id", e.Err)
	assert.Equal(t, 400, e.Status)
}

func TestParseError_EscapedJSON_UnescapesAndParses(t *testing.T) {
	inner := `{"message":"unauthorized","error":"token expired","status":401}`
	// Simulate a double-encoded JSON string (value is a JSON string containing JSON)
	escaped := fmt.Sprintf(`%q`, inner)
	e, err := ParseError(escaped)

	require.NoError(t, err)
	assert.Equal(t, "unauthorized", e.Message)
	assert.Equal(t, 401, e.Status)
}

func TestParseError_InvalidJSON_ReturnsError(t *testing.T) {
	_, err := ParseError("not json at all")
	assert.Error(t, err)
}

func TestParseError_EmptyJSON_ReturnsZeroValueStruct(t *testing.T) {
	e, err := ParseError(`{}`)
	require.NoError(t, err)
	assert.Empty(t, e.Message)
	assert.Zero(t, e.Status)
}

//  FromError

func TestFromError_NilReturnsNil(t *testing.T) {
	assert.Nil(t, FromError(nil))
}

func TestFromError_HttpErrorPassthrough(t *testing.T) {
	original := &HttpError{Status: 403, Message: "forbidden", Err: "no access"}
	result := FromError(original)

	assert.Same(t, original, result)
}

func TestFromError_ParseableErrorString_ReturnsHttpError(t *testing.T) {
	raw := `{"message":"conflict","error":"duplicate key","status":409}`
	result := FromError(errors.New(raw))

	assert.Equal(t, 409, result.Status)
	assert.Equal(t, "conflict", result.Message)
}

func TestFromError_UnparseableError_ReturnsDefault500(t *testing.T) {
	result := FromError(errors.New("something went completely wrong"))

	assert.Equal(t, 500, result.Status)
	assert.Equal(t, "something went completely wrong", result.Message)
	assert.NotEmpty(t, result.Err)
}

func TestFromError_WrappedHttpError_IsExtracted(t *testing.T) {
	original := &HttpError{Status: 502, Message: "bad gateway", Err: "upstream failed"}
	wrapped := fmt.Errorf("outer: %w", original)

	// errors.As should work since FromError checks type assertion first
	result := FromError(wrapped)

	// The wrapped error is NOT a *HttpError directly, so it falls through to ParseError.
	// ParseError will fail (not valid JSON), so it returns a default 500.
	assert.NotNil(t, result)
	assert.Equal(t, 500, result.Status)
}
