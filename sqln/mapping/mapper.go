package mapping

import (
	"database/sql"
	"reflect"

	"github.com/lib/pq"
)

// IsSimpleType retorna true se o valor não é struct nem pointer.
func IsSimpleType(value any) bool {
	kind := reflect.TypeOf(value).Kind()
	return kind != reflect.Struct && kind != reflect.Ptr
}

// GetContentList lê todas as linhas e faz scan para []T usando tags `gofi`.
func GetContentList[T any](rows *sql.Rows) ([]T, error) {
	list := make([]T, 0, 10)

	for rows.Next() {
		var err error
		var value T

		if IsSimpleType(value) {
			err = rows.Scan(&value)
		} else {
			model := new(T)
			err = rows.Scan(GetMappedCols(model)...)
			if err == nil {
				value = *model
			}
		}

		if err != nil {
			return nil, err
		}

		list = append(list, value)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return list, nil
}

// GetMappedCols retorna os endereços dos campos anotados com `db` para uso em rows.Scan.
func GetMappedCols(model any) []any {
	modelValue := reflect.ValueOf(model)
	modelType := reflect.TypeOf(model)

	if modelValue.Kind() != reflect.Ptr {
		modelPtr := reflect.New(modelType)
		modelPtr.Elem().Set(modelValue)
		modelValue = modelPtr
		modelType = modelPtr.Type()
	}

	modelValue = modelValue.Elem()
	modelType = modelType.Elem()

	if modelValue.Kind() != reflect.Struct {
		return []any{modelValue.Addr().Interface()}
	}

	cols := make([]any, 0, modelType.NumField())

	for i := 0; i < modelType.NumField(); i++ {
		field := modelValue.Field(i)
		fieldType := modelType.Field(i)

		if _, ok := fieldType.Tag.Lookup("db"); !ok {
			continue
		}

		if field.Kind() == reflect.Slice {
			cols = append(cols, pq.Array(field.Addr().Interface()))
		} else {
			cols = append(cols, field.Addr().Interface())
		}
	}

	return cols
}
