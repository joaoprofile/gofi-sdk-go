package common

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/joaoprofile/gofi/base/validator"
)

func ParseStructName(s interface{}) (string, error) {
	if err := validator.IsStruct(s); err != nil {
		return "", err
	}

	t := reflect.TypeOf(s)
	structName := t.Name()
	return CamelToSnake(structName), nil
}

func ParseStructColumns(s interface{}) (string, error) {
	if err := validator.IsStruct(s); err != nil {
		return "", err
	}

	queryType := reflect.TypeOf(s)
	var columns []string
	for i := 0; i < queryType.NumField(); i++ {
		field := queryType.Field(i)
		columnTag := field.Tag.Get("column")
		columns = append(columns, columnTag)
	}

	return fmt.Sprintf("%s", strings.Join(columns, ", ")), nil
}

var durationType = reflect.TypeOf(time.Duration(0))

func ParseStructAnnotation(cfg any, annotation string) error {
	if err := validator.IsStructP(cfg); err != nil {
		return err
	}

	v := reflect.ValueOf(cfg).Elem()
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)

		envName := fieldType.Tag.Get(annotation)
		if envName == "" {
			continue
		}

		envValue := os.Getenv(envName)
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
				return fmt.Errorf("error parsing duration for %s: %v", envName, err)
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
				return fmt.Errorf("error parsing bool for %s: %v", envName, err)
			}
			field.SetBool(val)

		case reflect.Int:
			val, err := strconv.Atoi(envValue)
			if err != nil {
				return fmt.Errorf("error parsing int for %s: %v", envName, err)
			}
			field.SetInt(int64(val))

		case reflect.Int64:
			val, err := strconv.ParseInt(envValue, 10, 64)
			if err != nil {
				return fmt.Errorf("error parsing int64 for %s: %v", envName, err)
			}
			field.SetInt(val)

		case reflect.Float64:
			val, err := strconv.ParseFloat(envValue, 64)
			if err != nil {
				return fmt.Errorf("error parsing float64 for %s: %v", envName, err)
			}
			field.SetFloat(val)

		default:
			return fmt.Errorf("unsupported field type %s (%s)", field.Kind(), fieldType.Name)
		}
	}

	return nil
}
