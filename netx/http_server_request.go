package netx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/gofi-labs/gofi-sdk-go/base/validator"
)

const ErrMsgOnQueryParameter = "error on parsing query parameters %w"

// Causes wrapped by RequestError, for errors.Is.
var (
	ErrUnsupportedMediaType = errors.New("unsupported media type")
	ErrBodyTooLarge         = errors.New("request body too large")
	ErrInvalidJSON          = errors.New("invalid JSON body")
	ErrInvalidQueryParam    = errors.New("invalid query parameter")
)

// RequestError is a client error. Error returns Message, which is safe to
// send to the client; Err keeps the detailed cause for logs (errors.Unwrap).
type RequestError struct {
	Status  int
	Message string
	Err     error
}

func (e *RequestError) Error() string { return e.Message }
func (e *RequestError) Unwrap() error { return e.Err }

func newRequestError(status int, kind error, message string, cause error) *RequestError {
	err := kind
	if cause != nil {
		err = fmt.Errorf("%w: %w", kind, cause)
	}
	return &RequestError{Status: status, Message: message, Err: err}
}

//	Body parsing
//
// ReadBody reads and returns the full request body.
// The body is always closed, even when the read fails.
func ReadBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}

// BodyOption tunes ParseRequestBody.
type BodyOption func(*bodyOptions)

type bodyOptions struct {
	allowUnknown bool
	maxBytes     int64
}

// AllowUnknownFields accepts JSON fields the target struct does not declare.
func AllowUnknownFields() BodyOption {
	return func(o *bodyOptions) { o.allowUnknown = true }
}

// WithMaxBodyBytes caps the body read by ParseRequestBody (default
// DefaultMaxBodyBytes); the server-wide WSConfig.MaxBodyBytes still applies.
func WithMaxBodyBytes(n int64) BodyOption {
	return func(o *bodyOptions) {
		if n > 0 {
			o.maxBytes = n
		}
	}
}

// ParseRequestBody decodes a single JSON value from the request body into
// tStruct, a non-nil pointer to a struct (ErrInvalidStruct otherwise). It
// requires Content-Type application/json (or application/*+json), rejects
// unknown fields (see AllowUnknownFields) and trailing data. Client errors
// are *RequestError: 415 for the media type, 413 for an oversized body and
// 400 for invalid JSON, with generic messages safe to return.
func ParseRequestBody(w http.ResponseWriter, r *http.Request, tStruct any, opts ...BodyOption) error {
	if err := validator.IsStructP(tStruct); err != nil {
		return ErrInvalidStruct
	}
	o := bodyOptions{maxBytes: DefaultMaxBodyBytes}
	for _, opt := range opts {
		opt(&o)
	}
	if err := requireJSON(r.Header.Get("Content-Type")); err != nil {
		return err
	}
	if r.Body == nil {
		return bodyError(io.EOF)
	}
	defer r.Body.Close()

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, o.maxBytes))
	if !o.allowUnknown {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(tStruct); err != nil {
		return bodyError(err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("trailing data after the JSON value")
		}
		return bodyError(err)
	}
	return nil
}

// requireJSON accepts application/json and application/*+json, with params.
func requireJSON(contentType string) error {
	mt, _, err := mime.ParseMediaType(contentType)
	if err == nil && (mt == "application/json" || strings.HasPrefix(mt, "application/") && strings.HasSuffix(mt, "+json")) {
		return nil
	}
	return newRequestError(http.StatusUnsupportedMediaType, ErrUnsupportedMediaType,
		"content type must be application/json", fmt.Errorf("content type %q", contentType))
}

func bodyError(err error) *RequestError {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return newRequestError(http.StatusRequestEntityTooLarge, ErrBodyTooLarge, ErrBodyTooLarge.Error(), err)
	}
	if errors.Is(err, io.EOF) {
		err = errors.New("empty body")
	}
	return newRequestError(http.StatusBadRequest, ErrInvalidJSON, ErrInvalidJSON.Error(), err)
}

// Query / path parameters
//
// GetQueryParam returns the value of the named URL query parameter, unchanged.
func GetQueryParam(filter string, r *http.Request) string {
	return r.URL.Query().Get(filter)
}

// GetPathParam returns the value of the named URL path parameter, unchanged.
func GetPathParam(param string, r *http.Request) string {
	return r.PathValue(param)
}

// BindQueryParamsToStruct populates tStruct from the URL query parameters.
// Each exported field is matched by its `form` tag ("-" skips it), falling
// back to the lowercased field name; unexported fields are ignored. tStruct
// must be a non-nil pointer to a struct. A value that does not parse or does
// not fit the field is a 400 *RequestError naming the parameter, never
// echoing the value.
func BindQueryParamsToStruct(r *http.Request, _ http.ResponseWriter, tStruct any) error {
	if err := validator.IsStructP(tStruct); err != nil {
		return fmt.Errorf(ErrMsgOnQueryParameter, err)
	}

	structType := reflect.TypeOf(tStruct).Elem()
	objValue := reflect.ValueOf(tStruct).Elem()
	queryParams := r.URL.Query()

	for i := range structType.NumField() {
		name, ok := queryParamName(structType.Field(i))
		if !ok {
			continue
		}
		vals := queryParams[name]
		if len(vals) == 0 {
			continue
		}
		if err := bindField(objValue.Field(i), vals); err != nil {
			return newRequestError(http.StatusBadRequest, ErrInvalidQueryParam,
				"invalid query parameter: "+name, fmt.Errorf("%s: %w", name, err))
		}
	}
	return nil
}

func queryParamName(field reflect.StructField) (string, bool) {
	if !field.IsExported() {
		return "", false
	}
	name := field.Tag.Get("form")
	if name == "-" {
		return "", false
	}
	if name == "" {
		name = strings.ToLower(field.Name)
	}
	return name, true
}

func bindField(value reflect.Value, vals []string) error {
	if value.Kind() == reflect.Slice {
		return setSliceFromStrings(value, vals)
	}
	return setFieldValue(value, vals[0])
}

// Field binding
//
// setFieldValue sets a struct field from its string query-parameter value,
// applying the appropriate conversion for the field's kind. Supported kinds:
// string, bool, int/int8..int64, uint/uint8..uint64, and pointers to any of
// these (the pointer is allocated on demand). Numbers must fit the field.
func setFieldValue(value reflect.Value, strValue string) error {
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			value.Set(reflect.New(value.Type().Elem()))
		}
		return setFieldValue(value.Elem(), strValue)
	}

	switch value.Kind() {
	case reflect.String:
		value.SetString(strValue)
		return nil
	case reflect.Bool:
		return setBool(value, strValue)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return setUint(value, strValue)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return setInt(value, strValue)
	default:
		return errors.New("unsupported kind")
	}
}

func setBool(value reflect.Value, s string) error {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return err
	}
	value.SetBool(v)
	return nil
}

func setUint(value reflect.Value, s string) error {
	v, err := strconv.ParseUint(s, 10, value.Type().Bits())
	if err != nil {
		return err
	}
	value.SetUint(v)
	return nil
}

func setInt(value reflect.Value, s string) error {
	v, err := strconv.ParseInt(s, 10, value.Type().Bits())
	if err != nil {
		return err
	}
	value.SetInt(v)
	return nil
}

// setSliceFromStrings populates a slice field from query-param values.
// Two wire formats are accepted:
//  1. JSON array literal in a single value (e.g. ?ids=[1,2,3]) — detected
//     when len(vals) == 1 and the trimmed value starts with "[".
//  2. Repeated params, optionally comma-separated within a single value
//     (e.g. ?ids=1&ids=2, ?ids=1,2,3, ?ids=1,2&ids=3).
//
// Element kinds supported match setFieldValue (string, bool, int*, uint*,
// and pointers to those). Slices of structs are left for a future PR.
func setSliceFromStrings(value reflect.Value, vals []string) error {
	if len(vals) == 1 && strings.HasPrefix(strings.TrimSpace(vals[0]), "[") {
		target := reflect.New(value.Type())
		if err := json.Unmarshal([]byte(vals[0]), target.Interface()); err != nil {
			return fmt.Errorf("invalid JSON array: %w", err)
		}
		value.Set(target.Elem())
		return nil
	}

	var expanded []string
	for _, v := range vals {
		for piece := range strings.SplitSeq(v, ",") {
			if piece = strings.TrimSpace(piece); piece != "" {
				expanded = append(expanded, piece)
			}
		}
	}
	if len(expanded) == 0 {
		return nil
	}

	slice := reflect.MakeSlice(value.Type(), len(expanded), len(expanded))
	for i, s := range expanded {
		if err := setFieldValue(slice.Index(i), s); err != nil {
			return fmt.Errorf("[%d]: %w", i, err)
		}
	}
	value.Set(slice)
	return nil
}
