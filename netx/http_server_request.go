package netx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/joaoprofile/gofi/base/validator"
)

const ErrMsgOnQueryParameter = "error on parsing query parameters %w"

//	Body parsing
//
// ReadBody reads and returns the full request body.
// The body is always closed, even when the read fails.
func ReadBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}

// ParseRequestBody decodes the JSON request body into tStruct.
// tStruct must be a non-nil pointer to a struct; otherwise ErrInvalidStruct is returned.
func ParseRequestBody(_ http.ResponseWriter, r *http.Request, tStruct interface{}) error {
	if err := validator.IsStructP(tStruct); err != nil {
		return ErrInvalidStruct
	}

	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}

	return json.Unmarshal(body, tStruct)
}

// Query / path parameters
//
// GetQueryParam returns the lowercased value of the named URL query parameter.
func GetQueryParam(filter string, r *http.Request) string {
	return strings.ToLower(r.URL.Query().Get(filter))
}

// GetPathParam returns the lowercased value of the named URL path parameter.
func GetPathParam(param string, r *http.Request) string {
	return strings.ToLower(r.PathValue(param))
}

// BindQueryParamsToStruct populates tStruct from the URL query parameters.
// Each exported field is matched by its `form` tag, falling back to the lowercased
// field name. tStruct must be a non-nil pointer to a struct.
func BindQueryParamsToStruct(r *http.Request, w http.ResponseWriter, tStruct interface{}) error {
	if err := validator.IsStructP(tStruct); err != nil {
		return fmt.Errorf(ErrMsgOnQueryParameter, err)
	}

	structType := reflect.TypeOf(tStruct).Elem()
	objValue := reflect.ValueOf(tStruct).Elem()
	queryParams := r.URL.Query()

	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		paramName := field.Tag.Get("form")
		if paramName == "" {
			paramName = strings.ToLower(field.Name)
		}
		if vals, ok := queryParams[paramName]; ok && len(vals) > 0 {
			if err := setFieldValue(objValue.Field(i), vals[0]); err != nil {
				return err
			}
		}
	}

	return nil
}

// Field binding
//
// setFieldValue sets a struct field from its string query-parameter value,
// applying the appropriate conversion for the field's kind.
func setFieldValue(value reflect.Value, strValue string) error {
	switch value.Kind() {
	case reflect.String:
		value.SetString(strValue)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v, err := strconv.ParseUint(strValue, 10, 64)
		if err != nil {
			return err
		}
		value.SetUint(v)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v, err := strconv.ParseInt(strValue, 10, 64)
		if err != nil {
			return err
		}
		value.SetInt(v)
	default:
		return errors.New("unsupported kind")
	}
	return nil
}
