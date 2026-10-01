package mapping

import (
	"database/sql"
	"iter"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

var (
	timeType    = reflect.TypeFor[time.Time]()
	scannerType = reflect.TypeFor[sql.Scanner]()
)

// fieldPlan describes a single leaf field within a cached struct plan. The
// indexPath resolves to the leaf via reflect.Value.FieldByIndex.
type fieldPlan struct {
	indexPath []int
	isSlice   bool   // scanned as a PostgreSQL array
	name      string // lower-cased db tag name; empty when untagged by name
}

// typePlan is the cached introspection result for a Go type. It contains the
// flat list of leaf fields — nested value objects are already unrolled — and
// pre-decided handling for time.Time / sql.Scanner / slices.
type typePlan struct {
	simple bool           // non-struct root — scan whole value as scalar
	fields []fieldPlan    // leaves to scan into, in declaration order
	byName map[string]int // field index by column name; nil when names are missing or ambiguous
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
	if len(p.fields) == 0 {
		return p
	}
	p.byName = make(map[string]int, len(p.fields))
	for i, f := range p.fields {
		if _, dup := p.byName[f.name]; f.name == "" || dup {
			p.byName = nil
			break
		}
		p.byName[f.name] = i
	}
	return p
}

// collectPlanFields walks struct fields recursively and appends a fieldPlan for
// each `db`-tagged leaf. Nested structs are descended into unless they are
// time.Time, implement sql.Scanner, or are slices (slices are leaves wrapped
// as PostgreSQL arrays at scan time). Byte slices ([]byte, json.RawMessage) are scalars
// at the driver level (bytea/json/jsonb), not Postgres arrays, so they take the
// default path — wrapping them as arrays breaks JSONB scans.
func collectPlanFields(t reflect.Type, prefix []int, out *[]fieldPlan) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if _, ok := f.Tag.Lookup("db"); !ok {
			continue
		}
		path := append(append([]int(nil), prefix...), i)
		name := tagName(f.Tag.Get("db"))
		switch {
		case reflect.PointerTo(f.Type).Implements(scannerType):
			*out = append(*out, fieldPlan{indexPath: path, name: name})
		case f.Type.Kind() == reflect.Slice && f.Type.Elem().Kind() == reflect.Uint8:
			*out = append(*out, fieldPlan{indexPath: path, name: name})
		case f.Type.Kind() == reflect.Slice:
			*out = append(*out, fieldPlan{indexPath: path, isSlice: true, name: name})
		case isNestedScannableType(f.Type):
			collectPlanFields(f.Type, path, out)
		default:
			*out = append(*out, fieldPlan{indexPath: path, name: name})
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
// Columns are matched by name when they cover every tagged field (extra columns
// are ignored); otherwise fields are scanned by position, as before.
func GetContentList[T any](rows *sql.Rows) ([]T, error) {
	list := make([]T, 0, 10)
	scan, err := newRowScanner[T](rows)
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		var value T
		if err := scan(&value); err != nil {
			return nil, err
		}
		list = append(list, value)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return list, nil
}

// Each streams rows as T values and closes rows when done or when the
// consumer stops early.
func Each[T any](rows *sql.Rows) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		defer rows.Close()
		var zero T
		scan, err := newRowScanner[T](rows)
		if err != nil {
			yield(zero, err)
			return
		}
		for rows.Next() {
			var value T
			if err := scan(&value); err != nil {
				yield(zero, err)
				return
			}
			if !yield(value, nil) {
				return
			}
		}
		if err := rows.Err(); err != nil {
			yield(zero, err)
		}
	}
}

// GetUnique scans the first row into a *T with the same rules as GetContentList.
// Returns (nil, nil) when there are no rows.
func GetUnique[T any](rows *sql.Rows) (*T, error) {
	scan, err := newRowScanner[T](rows)
	if err != nil {
		return nil, err
	}
	if !rows.Next() {
		return nil, rows.Err()
	}
	value := new(T)
	if err := scan(value); err != nil {
		return nil, err
	}
	return value, nil
}

// newRowScanner resolves the column layout once per result set.
func newRowScanner[T any](rows *sql.Rows) (func(*T) error, error) {
	var zero T
	if IsSimpleType(zero) {
		return func(v *T) error { return rows.Scan(v) }, nil
	}
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	plan := getTypePlan(reflect.TypeFor[T]())
	order := columnOrder(plan, cols)
	if order == nil {
		return func(v *T) error { return rows.Scan(GetMappedCols(v)...) }, nil
	}
	return func(v *T) error {
		rv := reflect.ValueOf(v).Elem()
		dest := make([]any, len(order))
		for i, fi := range order {
			if fi < 0 {
				dest[i] = new(any)
				continue
			}
			dest[i] = fieldAddr(rv, plan.fields[fi])
		}
		return rows.Scan(dest...)
	}, nil
}

var orderCache sync.Map // map[orderKey][]int

type orderKey struct {
	plan *typePlan
	cols string
}

// columnOrder maps each column to a field index (-1 = ignored). It returns nil,
// meaning positional scanning, when names cannot cover every field unambiguously.
func columnOrder(plan *typePlan, cols []string) []int {
	if plan.byName == nil {
		return nil
	}
	key := orderKey{plan, strings.Join(cols, "\x00")}
	if cached, ok := orderCache.Load(key); ok {
		return cached.([]int)
	}
	order := make([]int, len(cols))
	seen := make([]bool, len(plan.fields))
	covered := 0
	identity := len(cols) == len(plan.fields)
	for i, c := range cols {
		fi, ok := plan.byName[strings.ToLower(c)]
		if !ok || seen[fi] {
			order[i] = -1
			identity = false
			continue
		}
		seen[fi] = true
		order[i] = fi
		covered++
		identity = identity && fi == i
	}
	if covered != len(plan.fields) || identity {
		order = nil
	}
	orderCache.Store(key, order)
	return order
}

func fieldAddr(v reflect.Value, f fieldPlan) any {
	fv := v.FieldByIndex(f.indexPath)
	if f.isSlice {
		return pgArray{fv.Addr().Interface()}
	}
	return fv.Addr().Interface()
}

// typeMaps pools pgtype maps: a Map caches scan plans and is not safe for
// concurrent use.
var typeMaps = sync.Pool{New: func() any { return pgtype.NewMap() }}

// pgArray scans a PostgreSQL array (text format) into a slice pointer.
type pgArray struct{ dst any }

func (a pgArray) Scan(src any) error {
	m := typeMaps.Get().(*pgtype.Map)
	defer typeMaps.Put(m)
	return m.SQLScanner(a.dst).Scan(src)
}

func tagName(tag string) string {
	name, _, _ := strings.Cut(tag, ",")
	if name == "-" {
		return ""
	}
	return strings.ToLower(name)
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
		cols[i] = fieldAddr(v, f)
	}
	return cols
}
