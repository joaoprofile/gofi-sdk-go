package errs

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
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
	KindForbidden     ErrorKind = "FORBIDDEN"
)

type AppError struct {
	Kind    ErrorKind `json:"kind,omitempty"`
	Code    string    `json:"code"`
	Message string    `json:"message"`
	Details any       `json:"details,omitempty"`
	Err     error     `json:"-"`
}

var (
	errorMapMu sync.RWMutex
	errorMap   = map[string]AppError{}
)

func register(err AppError) AppError {
	errorMapMu.Lock()
	errorMap[err.Code] = err
	errorMapMu.Unlock()
	return err
}

// ---- Package level constructors ----
//
// New returns a copy of the reistred AppError, optionally formatting the message.
func New(code, message string, err error, args ...any) AppError {
	if len(args) > 0 {
		message = fmt.Sprintf(message, args...)
	}
	return AppError{Code: code, Message: message, Err: err}
}

// --- AppError methods ----
//
// productErrIdREquired.New()
// productErrIdREquired.New(args..)

func (e AppError) New(args ...any) AppError {
	result := e
	if len(args) > 0 {
		result.Message = fmt.Sprintf(result.Message, args...)
	}
	return result
}

// productErrIdREquired.New()
// productErrIdREquired.Wrap(error)
func (e AppError) Wrap(err error, args ...any) AppError {
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

func (e AppError) IsForbidden() bool { return e.Kind == KindForbidden }

func (e AppError) Exists() bool {
	return e.Code != "" || e.Err != nil
}

// GetSafeError returns the public Message as an error, or defaultMessage when
// Message is empty. The internal cause (Err) and Details are never exposed.
func (e AppError) GetSafeError(defaultMessage string) error {
	if e.Message != "" {
		return errors.New(e.Message)
	}
	return errors.New(defaultMessage)
}

// Error has a value receiver so AppError values satisfy error directly.
func (e AppError) Error() string {
	if e.Details != nil {
		return fmt.Sprintf("[%s:%s] %s: %v", e.Kind, e.Code, e.Message, e.Details)
	}
	return fmt.Sprintf("[%s:%s] %s", e.Kind, e.Code, e.Message)
}

// Unwrap exposes the wrapped cause to errors.Is and errors.As.
func (e AppError) Unwrap() error { return e.Err }

// Is matches another AppError (value or pointer) with the same non-empty Code,
// so errors.Is(err, ErrRegistered) works for errors built with New or Wrap.
func (e AppError) Is(target error) bool {
	var code string
	switch t := target.(type) {
	case AppError:
		code = t.Code
	case *AppError:
		if t == nil {
			return false
		}
		code = t.Code
	default:
		return false
	}
	return e.Code != "" && e.Code == code
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
	return register(AppError{Kind: KindUnknown, Code: code, Message: message})
}

func RegisterValidation(code, message string) AppError {
	return register(AppError{Kind: KindValidation, Code: code, Message: message})
}

func RegisterOperation(code, message string) AppError {
	return register(AppError{Kind: KindOperation, Code: code, Message: message})
}

func RegisterNotFound(code, message string) AppError {
	return register(AppError{Kind: KindNotFound, Code: code, Message: message})
}

func RegisterConflict(code, message string) AppError {
	return register(AppError{Kind: KindConflict, Code: code, Message: message})
}

func RegisterExternalError(code, message string) AppError {
	return register(AppError{Kind: KindExternalError, Code: code, Message: message})
}

func RegisterForbidden(code, message string) AppError {
	return register(AppError{Kind: KindForbidden, Code: code, Message: message})
}

func RegisterUnauthorized(code, message string) AppError {
	return register(AppError{Kind: KindUnauthorized, Code: code, Message: message})
}

func GetErrorByCode(code string) *AppError {
	errorMapMu.RLock()
	err, exists := errorMap[code]
	errorMapMu.RUnlock()
	if exists {
		return &err
	}
	return &AppError{}
}

// JoinErrors joins the non-nil errors as "a | b" and supports errors.Is/As on each.
// It returns a non-nil empty error when there are none (legacy contract); prefer errors.Join.
func JoinErrors(errs []error) error {
	kept := make([]error, 0, len(errs))
	msgs := make([]string, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			kept = append(kept, err)
			msgs = append(msgs, err.Error())
		}
	}
	return &joinedError{msg: strings.Join(msgs, " | "), errs: kept}
}

type joinedError struct {
	msg  string
	errs []error
}

func (e *joinedError) Error() string   { return e.msg }
func (e *joinedError) Unwrap() []error { return e.errs }
