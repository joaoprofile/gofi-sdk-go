package validator

import (
	"fmt"
	"reflect"
)

func IsStruct(s interface{}) error {
	if reflect.TypeOf(s).Kind() != reflect.Struct {
		return fmt.Errorf("expected a struct, got %T", s)
	}
	return nil
}

func IsStructP(s interface{}) error {
	if reflect.TypeOf(s).Kind() != reflect.Ptr || reflect.TypeOf(s).Elem().Kind() != reflect.Struct {
		return fmt.Errorf("expected a pointer to a struct, got %T", s)
	}
	return nil
}
