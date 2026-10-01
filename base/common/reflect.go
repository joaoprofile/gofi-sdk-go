package common

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/base/validator"
)

func ParseStructName(s any) (string, error) {
	if err := validator.IsStruct(s); err != nil {
		return "", err
	}

	t := reflect.TypeOf(s)
	structName := t.Name()
	return CamelToSnake(structName), nil
}

func ParseStructColumns(s any) (string, error) {
	if err := validator.IsStruct(s); err != nil {
		return "", err
	}

	queryType := reflect.TypeOf(s)
	var columns []string
	for field := range queryType.Fields() {
		columnTag := field.Tag.Get("column")
		columns = append(columns, columnTag)
	}

	return strings.Join(columns, ", "), nil
}

var durationType = reflect.TypeFor[time.Duration]()

func ParseStructAnnotation(cfg any, annotation string) error {
	return ParseStructAnnotationFunc(cfg, annotation, os.Getenv)
}

// ParseStructAnnotationFunc fills cfg from lookup(tag) and returns every
// conversion error joined, so all invalid variables are reported at once.
func ParseStructAnnotationFunc(cfg any, annotation string, lookup func(string) string) error {
	if err := validator.IsStructP(cfg); err != nil {
		return err
	}
	var errs []error

	v := reflect.ValueOf(cfg).Elem()
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)

		envName := fieldType.Tag.Get(annotation)
		if envName == "" {
			continue
		}

		envValue := lookup(envName)
		if envValue == "" {
			continue
		}

		isPtr := field.Kind() == reflect.Ptr
		if isPtr {
			if field.IsNil() {
				field.Set(reflect.New(field.Type().Elem()))
			}
			field = field.Elem()
		}

		ft := fieldType.Type
		kind := field.Kind()

		if ft == durationType {
			val, err := time.ParseDuration(envValue)
			if err != nil {
				errs = append(errs, parseError(envName, "duration", err))
				continue
			}
			field.SetInt(int64(val))
			continue
		}

		switch kind {
		case reflect.String:
			field.SetString(envValue)

		case reflect.Bool:
			val, err := strconv.ParseBool(envValue)
			if err != nil {
				errs = append(errs, parseError(envName, "bool", err))
				continue
			}
			field.SetBool(val)

		case reflect.Int:
			val, err := strconv.Atoi(envValue)
			if err != nil {
				errs = append(errs, parseError(envName, "int", err))
				continue
			}
			field.SetInt(int64(val))

		case reflect.Int64:
			val, err := strconv.ParseInt(envValue, 10, 64)
			if err != nil {
				errs = append(errs, parseError(envName, "int64", err))
				continue
			}
			field.SetInt(val)

		case reflect.Float64:
			val, err := strconv.ParseFloat(envValue, 64)
			if err != nil {
				errs = append(errs, parseError(envName, "float64", err))
				continue
			}
			field.SetFloat(val)

		default:
			errs = append(errs, fmt.Errorf("unsupported field type %s (%s)", field.Kind(), fieldType.Name))
		}
	}

	return errors.Join(errs...)
}

// parseError names the variable and the expected type but never the raw value,
// which may be a secret: strconv and time errors quote their input.
func parseError(name, typ string, err error) error {
	if ne, ok := errors.AsType[*strconv.NumError](err); ok {
		return fmt.Errorf("error parsing %s for %s: %w", typ, name, ne.Err)
	}
	return fmt.Errorf("error parsing %s for %s: invalid value", typ, name)
}
