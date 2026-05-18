package netx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/joaoprofile/gofi/base/errs"
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
	if appErr.Err != nil {
		response.Cause = appErr.Err.Error()
	}

	logAppError(r, status, appErr, response.Cause)

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
	if r != nil {
		attrs = append(attrs,
			slog.String("http.method", r.Method),
			slog.String("http.path", r.URL.Path),
		)
	}

	level := slog.LevelWarn
	if status >= http.StatusInternalServerError {
		level = slog.LevelError
	}
	slog.Default().Log(nil, level, "http error response", attrs...)
}

func Response(w http.ResponseWriter, statusCode int, data interface{}) {
	setJSONHeader(w)

	payload, err := json.Marshal(data)
	if err != nil {
		internalError(w)
		return
	}

	w.WriteHeader(statusCode)
	safeWrite(w, payload)
}

func Error(w http.ResponseWriter, statusCode int, err error) {
	writeError(w, statusCode, err, nil)
}

func ErrorDetails(w http.ResponseWriter, statusCode int, message string, details any) {
	writeError(w, statusCode, errors.New(message), details)
}

func writeError(w http.ResponseWriter, statusCode int, err error, details any) {
	response := ErrorResponse{
		StatusCode: statusCode,
		Message:    err.Error(),
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

func internalError(w http.ResponseWriter) {
	setJSONHeader(w)
	w.WriteHeader(http.StatusInternalServerError)

	safeWrite(w, []byte(`{"code":500,"message":"internal server error"}`))
}
