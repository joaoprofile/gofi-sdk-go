package netx

import (
	"encoding/json"
	"errors"
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

func RespondError(w http.ResponseWriter, appErr errs.AppError) {
	switch {
	case appErr.IsNotFound():
		writeError(w, http.StatusNotFound, &appErr, appErr.Details)
	case appErr.IsConflict():
		writeError(w, http.StatusConflict, &appErr, appErr.Details)
	case appErr.IsValidation():
		writeError(w, http.StatusBadRequest, &appErr, appErr.Details)
	default:
		writeError(w, http.StatusInternalServerError, &appErr, appErr.Details)
	}
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
