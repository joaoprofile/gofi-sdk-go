package filter

import (
	"fmt"
	"reflect"
)

// FieldMapping describes a field that is allowed in dynamic queries.
type FieldMapping struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	FilterType string `json:"filterType"`
	SearchType string `json:"searchType"`
}

// QueryMapping defines the complete allowlist for dynamic query validation:
// which fields can be filtered, which can be sorted, and which operators are permitted.
type QueryMapping struct {
	AllowedFields        []FieldMapping
	AllowedSortingFields map[string]string
	Operators            map[string]string
	LogicalOperators     map[string]string
}

// Validate checks that every field, condition, logical operator, and sorting field
// in the given Filters is within the configured allowlists.
func (qm *QueryMapping) Validate(items *Filters) error {
	if len(items.Filters) == 0 {
		return qm.validateSortField(items)
	}

	if err := qm.validateLogicalPlacement(items.Filters); err != nil {
		return err
	}

	allowedFieldMap := qm.buildFieldMap()

	for _, f := range items.Filters {
		if f.LogicalOperator != "" {
			if _, ok := qm.LogicalOperators[f.LogicalOperator]; !ok {
				return fmt.Errorf(errMsgInvalidLogicalOp, f.LogicalOperator)
			}
			continue
		}

		if _, found := allowedFieldMap[f.Field]; !found {
			return fmt.Errorf(errMsgInvalidField, f.Field)
		}

		if f.Condition != "" {
			if _, ok := qm.Operators[f.Condition]; !ok {
				return fmt.Errorf(errMsgInvalidCondition, f.Condition)
			}
		}

		if err := qm.validateValue(f.Value); err != nil {
			return err
		}
	}

	return qm.validateSortField(items)
}

func (qm *QueryMapping) validateLogicalPlacement(filters []*Filter) error {
	if len(filters) == 0 {
		return nil
	}

	if filters[0].LogicalOperator != "" {
		return fmt.Errorf(errMsgLeadingLogicalOp)
	}
	if filters[len(filters)-1].LogicalOperator != "" {
		return fmt.Errorf(errMsgTrailingLogicalOp)
	}

	for i := 1; i < len(filters); i++ {
		if filters[i].LogicalOperator != "" && filters[i-1].LogicalOperator != "" {
			return fmt.Errorf(errMsgConsecutiveLogicalOp)
		}
	}

	return nil
}

// validateValue checks a filter value (scalar or slice) against the SQL injection blocklist.
func (qm *QueryMapping) validateValue(value any) error {
	if value == nil {
		return nil
	}

	rv := reflect.ValueOf(value)
	if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
		for i := range rv.Len() {
			elem := rv.Index(i).Interface()
			if s, ok := elem.(string); ok && ContainsNotAllowedValue(s) {
				return fmt.Errorf(errMsgActionNotAllowed, s)
			}
		}
		return nil
	}

	if s, ok := value.(string); ok && ContainsNotAllowedValue(s) {
		return fmt.Errorf(errMsgActionNotAllowed, s)
	}

	return nil
}

func (qm *QueryMapping) validateSortField(items *Filters) error {
	if items.Params != nil && items.Params.SortField != "" {
		if _, ok := qm.AllowedSortingFields[items.Params.SortField]; !ok {
			return fmt.Errorf(errMsgInvalidSortingField, items.Params.SortField)
		}
	}
	return nil
}

func (qm *QueryMapping) buildFieldMap() map[string]struct{} {
	m := make(map[string]struct{}, len(qm.AllowedFields))
	for _, field := range qm.AllowedFields {
		m[field.Key] = struct{}{}
	}
	return m
}
