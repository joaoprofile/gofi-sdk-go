package errs

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrorKind classifies the category of an AppErro with a readable string identifier.
// This value appears verbatim i logs and JSON payloads, aiding observalitiry
type ErrorKind string

const (
	KindUnknown       ErrorKind = "UNKNOWN"
	KindValidation    ErrorKind = "VALIDATION"
	KindNotFound      ErrorKind = "NOT_FOUND"
	KindConflict      ErrorKind = "CONFLICT"
	KindOperation     ErrorKind = "OPERATION"
	KindExternalError ErrorKind = "EXTERNAL_ERROR"
	KindUnauthorized  ErrorKind = "UNAUTHORIZED"
)

type AppError struct {
	Kind    ErrorKind `json:"kind,omitempty"`
	Code    string    `json:"code"`
	Message string    `json:"message"`
	Details any       `json:"details,omitempty"`
	Err     error     `json:"-"`
}

var errorMap = map[string]AppError{}

// ---- Package level constructors ----
//
// New returns a copy of the reistred AppError, optionally formatting the message.
func New(code, message string, err error, args ...interface{}) AppError {
	if len(args) > 0 {
		message = fmt.Sprintf(message, args...)
	}
	return AppError{Code: code, Message: message, Err: err}
}

// --- AppError methods ----
//
// productErrIdREquired.New()
// productErrIdREquired.New(args..)

func (e AppError) New(args ...interface{}) AppError {
	result := e
	if len(args) > 0 {
		result.Message = fmt.Sprintf(result.Message, args...)
	}
	return result
}

// productErrIdREquired.New()
// productErrIdREquired.Wrap(error)
func (e AppError) Wrap(err error, args ...interface{}) AppError {
	result := e
	if len(args) > 0 {
		result.Message = fmt.Sprintf(result.Message, args...)
	}
	result.Err = err
	return result
}

func (e AppError) WithDetails(details any) AppError {
	result := e
	result.Details = details
	return result
}

// IsValidation reports whether this is a VALIDATION eror
func (e AppError) IsValidation() bool { return e.Kind == KindValidation }

func (e AppError) IsNotFound() bool { return e.Kind == KindNotFound }

func (e AppError) IsConflict() bool { return e.Kind == KindConflict }

func (e AppError) IsOperation() bool { return e.Kind == KindOperation }

func (e AppError) IsExternalError() bool { return e.Kind == KindExternalError }

func (e AppError) IsUnauthorized() bool { return e.Kind == KindUnauthorized }

func (e AppError) Exists() bool {
	return e.Code != "" || e.Err != nil
}

func (e AppError) GetSafeError(defaultMessage string) error {
	if e.Err != nil {
		return e.Err
	}
	return errors.New(defaultMessage)
}

func (e *AppError) Error() string {
	if e == nil {
		return ""
	}
	if e.Details != nil {
		return fmt.Sprintf("[%s:%s] %s: %v", e.Kind, e.Code, e.Message, e.Details)
	}
	return fmt.Sprintf("[%s:%s] %s", e.Kind, e.Code, e.Message)
}

func (e *AppError) GetCode() string {
	return e.Code
}

func (e *AppError) ErrorString() string {
	if e.Details != nil {
		return fmt.Sprintf("[%s:%s] %s: %v", e.Kind, e.Code, e.Message, e.Details)
	}
	return fmt.Sprintf("[%s:%s] %s", e.Kind, e.Code, e.Message)
}

func (e *AppError) ToJSON() string {
	data, _ := json.Marshal(e)
	return string(data)
}

func Register(code, message string) AppError {
	err := AppError{Kind: KindUnknown, Code: code, Message: message}
	errorMap[code] = err
	return err
}

func RegisterValidation(code, message string) AppError {
	err := AppError{Kind: KindValidation, Code: code, Message: message}
	errorMap[code] = err
	return err
}

func RegisterOperation(code, message string) AppError {
	err := AppError{Kind: KindOperation, Code: code, Message: message}
	errorMap[code] = err
	return err
}

func RegisterNotFound(code, message string) AppError {
	err := AppError{Kind: KindNotFound, Code: code, Message: message}
	errorMap[code] = err
	return err
}

func RegisterConflict(code, message string) AppError {
	err := AppError{Kind: KindConflict, Code: code, Message: message}
	errorMap[code] = err
	return err
}

func RegisterExternalError(code, message string) AppError {
	err := AppError{Kind: KindExternalError, Code: code, Message: message}
	errorMap[code] = err
	return err
}

func RegisterUnauthorized(code, message string) AppError {
	err := AppError{Kind: KindUnauthorized, Code: code, Message: message}
	errorMap[code] = err
	return err
}

func GetErrorByCode(code string) *AppError {
	if err, exists := errorMap[code]; exists {
		return &err
	}
	return &AppError{}
}

func JoinErrors(errs []error) error {
	msgs := make([]string, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			msgs = append(msgs, err.Error())
		}
	}
	return fmt.Errorf("%s", strings.Join(msgs, " | "))
}
