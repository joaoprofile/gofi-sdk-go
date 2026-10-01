package netx

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/joaoprofile/gofi-sdk-go/base/errs"
)

var (
	ErrJSONEncoder       = errors.New("error encoding data")
	ErrReadBody          = errors.New("error reading request body")
	ErrJSONUnmarshal     = errors.New("JSON structure error")
	ErrInvalidStruct     = errors.New("provided type is not a valid struct")
	ErrMissingQueryParam = errors.New("missing query parameter")
)

type ErrorResponse struct {
	StatusCode int    `json:"code"`
	Message    string `json:"message"`
	ErrorCode  string `json:"errorCode,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Cause      string `json:"cause,omitempty"`
	Details    any    `json:"details,omitempty"`
}

func setJSONHeader(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
}

func safeWrite(w http.ResponseWriter, data []byte) {
	_, _ = w.Write(data)
}

func JSON(w http.ResponseWriter, statusCode int, jsonData []byte) {
	if jsonData == nil {
		w.WriteHeader(statusCode)
		return
	}

	setJSONHeader(w)
	w.WriteHeader(statusCode)

	safeWrite(w, jsonData)
}

func RespondError(w http.ResponseWriter, r *http.Request, appErr errs.AppError) {
	var status int
	switch {
	case appErr.IsNotFound():
		status = http.StatusNotFound
	case appErr.IsConflict():
		status = http.StatusConflict
	case appErr.IsValidation():
		status = http.StatusBadRequest
	case appErr.IsUnauthorized():
		status = http.StatusUnauthorized
	case appErr.IsForbidden():
		status = http.StatusForbidden
	case appErr.IsExternalError():
		status = http.StatusBadGateway
	default:
		status = http.StatusInternalServerError
	}

	response := ErrorResponse{
		StatusCode: status,
		Message:    appErr.Message,
		ErrorCode:  appErr.Code,
		Kind:       string(appErr.Kind),
		Details:    appErr.Details,
	}

	// The wrapped cause may hold SQL or driver details; it is always logged.
	var cause string
	if appErr.Err != nil {
		cause = appErr.Err.Error()
	}
	if exposeCause(r) {
		response.Cause = cause
	}
	logAppError(r, status, appErr, cause)

	payload, marshalErr := json.Marshal(response)
	if marshalErr != nil {
		internalError(w)
		return
	}

	setJSONHeader(w)
	w.WriteHeader(status)
	safeWrite(w, payload)
}

func logAppError(r *http.Request, status int, appErr errs.AppError, cause string) {
	attrs := []any{
		slog.Int("http.status", status),
		slog.String("errorCode", appErr.Code),
		slog.String("kind", string(appErr.Kind)),
		slog.String("message", appErr.Message),
	}
	if cause != "" {
		attrs = append(attrs, slog.String("cause", cause))
	}
	ctx := context.Background()
	if r != nil {
		ctx = r.Context()
		attrs = append(attrs,
			slog.String("http.method", r.Method),
			slog.String("http.path", r.URL.Path),
		)
	}

	level := slog.LevelWarn
	if status >= http.StatusInternalServerError {
		level = slog.LevelError
	}
	slog.Default().Log(ctx, level, "http error response", attrs...)
}

func Response(w http.ResponseWriter, statusCode int, data any) {
	setJSONHeader(w)

	payload, err := json.Marshal(data)
	if err != nil {
		internalError(w)
		return
	}

	w.WriteHeader(statusCode)
	safeWrite(w, payload)
}

// Error writes err as an ErrorResponse. A *RequestError (ParseRequestBody,
// BindQueryParamsToStruct) sets its own status and client-safe message. For
// status >= 500 the client gets a generic message and err is only logged.
func Error(w http.ResponseWriter, statusCode int, err error) {
	writeError(w, statusCode, err, nil)
}

// ErrorDetails writes message and details; for status >= 500 both are
// replaced by a generic message and logged.
func ErrorDetails(w http.ResponseWriter, statusCode int, message string, details any) {
	writeError(w, statusCode, errors.New(message), details)
}

func writeError(w http.ResponseWriter, statusCode int, err error, details any) {
	message := genericMessage(statusCode)
	var reqErr *RequestError
	switch {
	case errors.As(err, &reqErr):
		statusCode, message = reqErr.Status, reqErr.Message
	case err != nil:
		message = err.Error()
	}
	if statusCode >= http.StatusInternalServerError {
		slog.Error("http error response", slog.Int("http.status", statusCode), slog.Any("error", err))
		message, details = genericMessage(statusCode), nil
	}

	response := ErrorResponse{
		StatusCode: statusCode,
		Message:    message,
		Details:    details,
	}

	payload, marshalErr := json.Marshal(response)
	if marshalErr != nil {
		internalError(w)
		return
	}

	setJSONHeader(w)
	w.WriteHeader(statusCode)
	safeWrite(w, payload)
}

// genericMessage is the lower-case status text, e.g. "internal server error".
func genericMessage(status int) string {
	if text := http.StatusText(status); text != "" {
		return strings.ToLower(text)
	}
	return "error"
}

func internalError(w http.ResponseWriter) {
	setJSONHeader(w)
	w.WriteHeader(http.StatusInternalServerError)

	safeWrite(w, []byte(`{"code":500,"message":"internal server error"}`))
}
