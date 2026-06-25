package mapping

import (
	"database/sql"
	"reflect"
	"sync"
	"time"

	"github.com/lib/pq"
)

var (
	timeType    = reflect.TypeOf(time.Time{})
	scannerType = reflect.TypeOf((*sql.Scanner)(nil)).Elem()
)

// fieldPlan describes a single leaf field within a cached struct plan. The
// indexPath resolves to the leaf via reflect.Value.FieldByIndex.
type fieldPlan struct {
	indexPath []int
	isSlice   bool // wrap the field's address with pq.Array on scan
}

// typePlan is the cached introspection result for a Go type. It contains the
// flat list of leaf fields — nested value objects are already unrolled — and
// pre-decided handling for time.Time / sql.Scanner / slices.
type typePlan struct {
	simple bool        // non-struct root — scan whole value as scalar
	fields []fieldPlan // leaves to scan into, in declaration order
}

var planCache sync.Map // map[reflect.Type]*typePlan

// getTypePlan returns the cached plan for t, building it on first use.
// Thread-safe via sync.Map.LoadOrStore — concurrent builders race harmlessly.
func getTypePlan(t reflect.Type) *typePlan {
	if cached, ok := planCache.Load(t); ok {
		return cached.(*typePlan)
	}
	plan := buildTypePlan(t)
	actual, _ := planCache.LoadOrStore(t, plan)
	return actual.(*typePlan)
}

func buildTypePlan(t reflect.Type) *typePlan {
	if t.Kind() != reflect.Struct {
		return &typePlan{simple: true}
	}
	p := &typePlan{}
	collectPlanFields(t, nil, &p.fields)
	return p
}

// collectPlanFields walks struct fields recursively and appends a fieldPlan for
// each `db`-tagged leaf. Nested structs are descended into unless they are
// time.Time, implement sql.Scanner, or are slices (slices are leaves wrapped
// with pq.Array at scan time). Byte slices ([]byte, json.RawMessage) are scalars
// at the driver level (bytea/json/jsonb), not Postgres arrays, so they take the
// default path — wrapping them in pq.Array breaks JSONB scans.
func collectPlanFields(t reflect.Type, prefix []int, out *[]fieldPlan) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if _, ok := f.Tag.Lookup("db"); !ok {
			continue
		}
		path := append(append([]int(nil), prefix...), i)
		switch {
		case reflect.PointerTo(f.Type).Implements(scannerType):
			*out = append(*out, fieldPlan{indexPath: path})
		case f.Type.Kind() == reflect.Slice && f.Type.Elem().Kind() == reflect.Uint8:
			*out = append(*out, fieldPlan{indexPath: path})
		case f.Type.Kind() == reflect.Slice:
			*out = append(*out, fieldPlan{indexPath: path, isSlice: true})
		case isNestedScannableType(f.Type):
			collectPlanFields(f.Type, path, out)
		default:
			*out = append(*out, fieldPlan{indexPath: path})
		}
	}
}

// isNestedScannableType returns true if t is a struct whose `db`-tagged
// sub-fields should be expanded. time.Time and types implementing sql.Scanner
// are treated as primitives (the driver scans them directly).
func isNestedScannableType(t reflect.Type) bool {
	if t.Kind() != reflect.Struct {
		return false
	}
	if t == timeType {
		return false
	}
	if reflect.PointerTo(t).Implements(scannerType) {
		return false
	}
	return true
}

// IsSimpleType returns true if the value is neither a struct nor a pointer.
func IsSimpleType(value any) bool {
	kind := reflect.TypeOf(value).Kind()
	return kind != reflect.Struct && kind != reflect.Ptr
}

// GetContentList reads all rows and scans them into []T using `db` tags.
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

// GetMappedCols returns the addresses of fields tagged with `db` for use in
// rows.Scan. The struct's leaf layout is cached per type — the first call for
// a given type builds the plan, subsequent calls reuse it.
//
// Nested structs tagged with `db` are expanded recursively — their `db`-tagged
// sub-fields become scan columns. time.Time and types implementing sql.Scanner
// are treated as primitive values.
func GetMappedCols(model any) []any {
	v := reflect.ValueOf(model)

	if v.Kind() != reflect.Ptr {
		tmp := reflect.New(v.Type())
		tmp.Elem().Set(v)
		v = tmp
	}
	v = v.Elem()

	plan := getTypePlan(v.Type())

	if plan.simple {
		return []any{v.Addr().Interface()}
	}

	cols := make([]any, len(plan.fields))
	for i, f := range plan.fields {
		fv := v.FieldByIndex(f.indexPath)
		if f.isSlice {
			cols[i] = pq.Array(fv.Addr().Interface())
		} else {
			cols[i] = fv.Addr().Interface()
		}
	}
	return cols
}
