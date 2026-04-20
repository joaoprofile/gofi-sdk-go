package errs_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/joaoprofile/gofi/base/errs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//  ErrorKind constants

func TestErrorKind_StringValues(t *testing.T) {
	assert.Equal(t, errs.ErrorKind("UNKNOWN"), errs.KindUnknown)
	assert.Equal(t, errs.ErrorKind("VALIDATION"), errs.KindValidation)
	assert.Equal(t, errs.ErrorKind("NOT_FOUND"), errs.KindNotFound)
	assert.Equal(t, errs.ErrorKind("CONFLICT"), errs.KindConflict)
	assert.Equal(t, errs.ErrorKind("OPERATION"), errs.KindOperation)
	assert.Equal(t, errs.ErrorKind("EXTERNAL_ERROR"), errs.KindExternalError)
	assert.Equal(t, errs.ErrorKind("UNAUTHORIZED"), errs.KindUnauthorized)
}

//  Register*

func TestRegister_SetsKindUnknown(t *testing.T) {
	e := errs.Register("reg-unknown", "message")
	assert.Equal(t, errs.KindUnknown, e.Kind)
	assert.Equal(t, "reg-unknown", e.Code)
	assert.Equal(t, "message", e.Message)
}

func TestRegisterValidation_SetsKind(t *testing.T) {
	e := errs.RegisterValidation("reg-validation", "field is required")
	assert.Equal(t, errs.KindValidation, e.Kind)
	assert.Equal(t, "reg-validation", e.Code)
}

func TestRegisterOperation_SetsKind(t *testing.T) {
	e := errs.RegisterOperation("reg-operation", "operation failed")
	assert.Equal(t, errs.KindOperation, e.Kind)
}

func TestRegisterNotFound_SetsKind(t *testing.T) {
	e := errs.RegisterNotFound("reg-not-found", "resource not found")
	assert.Equal(t, errs.KindNotFound, e.Kind)
}

func TestRegisterConflict_SetsKind(t *testing.T) {
	e := errs.RegisterConflict("reg-conflict", "resource already exists")
	assert.Equal(t, errs.KindConflict, e.Kind)
}

func TestRegisterExternalError_SetsKind(t *testing.T) {
	e := errs.RegisterExternalError("reg-external", "external API failed")
	assert.Equal(t, errs.KindExternalError, e.Kind)
}

func TestRegisterUnauthorized_SetsKind(t *testing.T) {
	e := errs.RegisterUnauthorized("reg-unauthorized", "access denied")
	assert.Equal(t, errs.KindUnauthorized, e.Kind)
}

//  GetErrorByCode──

func TestGetErrorByCode_ReturnsRegistered(t *testing.T) {
	errs.Register("lookup-existing", "lookup message")
	got := errs.GetErrorByCode("lookup-existing")
	require.NotNil(t, got)
	assert.Equal(t, "lookup-existing", got.Code)
}

func TestGetErrorByCode_MissingCodeReturnsEmpty(t *testing.T) {
	got := errs.GetErrorByCode("nonexistent-code-xyz")
	assert.False(t, got.Exists())
}

//  AppError.New (method) ─

func TestAppError_New_ReturnsCopyWithSameFields(t *testing.T) {
	base := errs.RegisterValidation("new-copy", "field is required")
	got := base.New()

	assert.Equal(t, base.Kind, got.Kind)
	assert.Equal(t, base.Code, got.Code)
	assert.Equal(t, base.Message, got.Message)
	assert.Nil(t, got.Err)
}

func TestAppError_New_FormatsMessage(t *testing.T) {
	base := errs.RegisterValidation("new-fmt", "value %s is invalid")
	got := base.New("foo")

	assert.Equal(t, "value foo is invalid", got.Message)
	assert.Equal(t, base.Code, got.Code)
	assert.Equal(t, base.Kind, got.Kind)
}

func TestAppError_New_DoesNotMutateOriginal(t *testing.T) {
	base := errs.RegisterValidation("new-immutable", "original message")
	_ = base.New("mutated")

	assert.Equal(t, "original message", base.Message)
}

func TestAppError_New_NoArgsPreservesMessage(t *testing.T) {
	base := errs.RegisterValidation("new-noargs", "static message")
	got := base.New()

	assert.Equal(t, "static message", got.Message)
}

//  AppError.Wrap

func TestAppError_Wrap_SetsUnderlyingError(t *testing.T) {
	cause := errors.New("db connection refused")
	base := errs.RegisterOperation("wrap-err", "operation failed")
	got := base.Wrap(cause)

	assert.Equal(t, cause, got.Err)
	assert.Equal(t, base.Code, got.Code)
	assert.Equal(t, base.Kind, got.Kind)
}

func TestAppError_Wrap_FormatsMessage(t *testing.T) {
	cause := errors.New("timeout")
	base := errs.RegisterOperation("wrap-fmt", "failed for id %s")
	got := base.Wrap(cause, "abc-123")

	assert.Equal(t, "failed for id abc-123", got.Message)
	assert.Equal(t, cause, got.Err)
}

func TestAppError_Wrap_DoesNotMutateOriginal(t *testing.T) {
	cause := errors.New("cause")
	base := errs.RegisterOperation("wrap-immutable", "original")
	_ = base.Wrap(cause, "mutated")

	assert.Equal(t, "original", base.Message)
	assert.Nil(t, base.Err)
}

func TestAppError_Wrap_NilErrorPreservesExists(t *testing.T) {
	base := errs.RegisterOperation("wrap-nil", "operation failed")
	got := base.Wrap(nil)

	assert.Nil(t, got.Err)
	assert.True(t, got.Exists()) // Code is still set
}

//  AppError.WithDetails

func TestAppError_WithDetails_AttachesDetails(t *testing.T) {
	base := errs.RegisterValidation("details-test", "validation failed")
	details := map[string]string{"field": "email", "reason": "invalid format"}
	got := base.WithDetails(details)

	assert.Equal(t, details, got.Details)
}

func TestAppError_WithDetails_DoesNotMutateOriginal(t *testing.T) {
	base := errs.RegisterValidation("details-immutable", "validation failed")
	_ = base.WithDetails("extra")

	assert.Nil(t, base.Details)
}

//  IsXxx kind checks

func TestAppError_IsValidation(t *testing.T) {
	e := errs.RegisterValidation("is-v", "msg")
	assert.True(t, e.IsValidation())
	assert.False(t, e.IsOperation())
	assert.False(t, e.IsNotFound())
	assert.False(t, e.IsConflict())
	assert.False(t, e.IsExternalError())
	assert.False(t, e.IsUnauthorized())
}

func TestAppError_IsNotFound(t *testing.T) {
	e := errs.RegisterNotFound("is-nf", "msg")
	assert.True(t, e.IsNotFound())
	assert.False(t, e.IsValidation())
}

func TestAppError_IsConflict(t *testing.T) {
	e := errs.RegisterConflict("is-c", "msg")
	assert.True(t, e.IsConflict())
	assert.False(t, e.IsValidation())
}

func TestAppError_IsOperation(t *testing.T) {
	e := errs.RegisterOperation("is-op", "msg")
	assert.True(t, e.IsOperation())
	assert.False(t, e.IsValidation())
}

func TestAppError_IsExternalError(t *testing.T) {
	e := errs.RegisterExternalError("is-ext", "msg")
	assert.True(t, e.IsExternalError())
	assert.False(t, e.IsOperation())
}

func TestAppError_IsUnauthorized(t *testing.T) {
	e := errs.RegisterUnauthorized("is-unauth", "msg")
	assert.True(t, e.IsUnauthorized())
	assert.False(t, e.IsValidation())
}

func TestAppError_Unknown_NoKindMatch(t *testing.T) {
	e := errs.Register("is-unknown", "msg")
	assert.False(t, e.IsValidation())
	assert.False(t, e.IsOperation())
	assert.False(t, e.IsNotFound())
	assert.False(t, e.IsConflict())
	assert.False(t, e.IsExternalError())
	assert.False(t, e.IsUnauthorized())
}

//  Exists─

func TestAppError_Exists_TrueWhenCodeSet(t *testing.T) {
	e := errs.RegisterValidation("exists-code", "msg")
	assert.True(t, e.Exists())
}

func TestAppError_Exists_TrueWhenErrSet(t *testing.T) {
	e := errs.AppError{Err: errors.New("some error")}
	assert.True(t, e.Exists())
}

func TestAppError_Exists_FalseWhenEmpty(t *testing.T) {
	e := errs.AppError{}
	assert.False(t, e.Exists())
}

//  Error / ErrorString

func TestAppError_Error_IncludesKindCodeAndMessage(t *testing.T) {
	e := errs.RegisterValidation("err-str", "field is required")
	str := e.Error()

	assert.Contains(t, str, "VALIDATION")
	assert.Contains(t, str, "err-str")
	assert.Contains(t, str, "field is required")
}

func TestAppError_Error_IncludesDetails(t *testing.T) {
	e := errs.RegisterOperation("err-details", "operation failed").WithDetails("extra context")
	str := e.Error()

	assert.Contains(t, str, "extra context")
}

func TestAppError_Error_Format(t *testing.T) {
	e := errs.RegisterOperation("err-fmt-check", "something went wrong")
	assert.Equal(t, "[OPERATION:err-fmt-check] something went wrong", e.Error())
}

func TestAppError_ErrorString_MatchesError(t *testing.T) {
	e := errs.RegisterOperation("err-string-match", "operation failed")
	assert.Equal(t, e.Error(), e.ErrorString())
}

func TestAppError_GetCode_ReturnsCode(t *testing.T) {
	e := errs.RegisterValidation("get-code", "msg")
	assert.Equal(t, "get-code", e.GetCode())
}

//  ToJSON─

func TestAppError_ToJSON_ContainsKindAndCode(t *testing.T) {
	e := errs.RegisterValidation("json-test", "invalid input")
	j := e.ToJSON()

	assert.Contains(t, j, `"kind":"VALIDATION"`)
	assert.Contains(t, j, `"code":"json-test"`)
	assert.Contains(t, j, `"message":"invalid input"`)
}

func TestAppError_ToJSON_OmitsEmptyKind(t *testing.T) {
	// KindUnknown = "UNKNOWN" — not empty string, so it will appear.
	// An AppError with zero Kind (empty string) should omit it.
	e := errs.AppError{Code: "no-kind", Message: "msg"} // Kind is ""
	j := e.ToJSON()

	assert.NotContains(t, j, `"kind"`)
}

func TestAppError_ToJSON_IncludesDetails(t *testing.T) {
	e := errs.RegisterOperation("json-details", "msg").WithDetails("some detail")
	j := e.ToJSON()

	assert.Contains(t, j, "some detail")
}

//  GetSafeError─

func TestAppError_GetSafeError_ReturnsWrappedErr(t *testing.T) {
	cause := errors.New("original cause")
	e := errs.RegisterOperation("safe-err", "msg").Wrap(cause)
	got := e.GetSafeError("default")

	assert.Equal(t, cause, got)
}

func TestAppError_GetSafeError_ReturnsDefaultWhenNoErr(t *testing.T) {
	e := errs.RegisterValidation("safe-default", "msg")
	got := e.GetSafeError("fallback message")

	assert.EqualError(t, got, "fallback message")
}

//  errs.New (package function)

func TestNew_Function_BuildsAppError(t *testing.T) {
	cause := errors.New("underlying")
	e := errs.New("fn-new", "message here", cause)

	assert.Equal(t, "fn-new", e.Code)
	assert.Equal(t, "message here", e.Message)
	assert.Equal(t, cause, e.Err)
	assert.Equal(t, errs.ErrorKind(""), e.Kind) // package-level New() does not set a kind
}

func TestNew_Function_FormatsMessage(t *testing.T) {
	e := errs.New("fn-new-fmt", "value %s for id %d", nil, "active", 42)
	assert.Equal(t, "value active for id 42", e.Message)
}

//  JoinErrors

func TestJoinErrors_CombinesMessages(t *testing.T) {
	list := []error{
		errors.New("first"),
		errors.New("second"),
		errors.New("third"),
	}
	joined := errs.JoinErrors(list)

	assert.Contains(t, joined.Error(), "first")
	assert.Contains(t, joined.Error(), "second")
	assert.Contains(t, joined.Error(), "third")
	assert.True(t, strings.Contains(joined.Error(), " | "))
}

func TestJoinErrors_SkipsNilErrors(t *testing.T) {
	list := []error{errors.New("valid"), nil, errors.New("also valid")}
	joined := errs.JoinErrors(list)

	assert.NotContains(t, joined.Error(), "nil")
	assert.Contains(t, joined.Error(), "valid")
	assert.Contains(t, joined.Error(), "also valid")
}

func TestJoinErrors_EmptySliceReturnsNoError(t *testing.T) {
	joined := errs.JoinErrors([]error{})
	assert.NotNil(t, joined)
	assert.Equal(t, "", joined.Error())
}
