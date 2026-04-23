package mapping

import (
	"reflect"
	"testing"
	"time"

	"github.com/lib/pq"
)

// Reference implementation (no plan cache) — matches the pre-Phase-1 behavior
// of GetMappedCols. Used only in benchmarks to measure the speedup delivered
// by the plan cache in production GetMappedCols.

func getMappedColsNoCache(model any) []any {
	v := reflect.ValueOf(model)

	if v.Kind() != reflect.Ptr {
		tmp := reflect.New(v.Type())
		tmp.Elem().Set(v)
		v = tmp
	}
	v = v.Elem()

	if v.Kind() != reflect.Struct {
		return []any{v.Addr().Interface()}
	}
	return collectColsNoCache(v, v.Type())
}

func collectColsNoCache(v reflect.Value, t reflect.Type) []any {
	cols := make([]any, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		fv := v.Field(i)
		ft := t.Field(i)
		if _, ok := ft.Tag.Lookup("db"); !ok {
			continue
		}
		switch {
		case fv.Kind() == reflect.Slice:
			cols = append(cols, pq.Array(fv.Addr().Interface()))
		case isNestedScannableType(fv.Type()):
			cols = append(cols, collectColsNoCache(fv, fv.Type())...)
		default:
			cols = append(cols, fv.Addr().Interface())
		}
	}
	return cols
}

// Benchmark structs — progressively more expensive to introspect.

type benchSimple struct {
	ID     int64  `db:"id"`
	Name   string `db:"name"`
	Email  string `db:"email"`
	Age    int    `db:"age"`
	Active bool   `db:"active"`
}

type benchPricing struct {
	Amount   float64 `db:"amount"`
	Currency string  `db:"currency"`
}

type benchWithVO struct {
	ID    int64        `db:"id"`
	Name  string       `db:"name"`
	Price benchPricing `db:"price"`
}

type benchInner struct {
	Leaf string `db:"leaf"`
}

type benchMiddle struct {
	Inner benchInner `db:"inner"`
}

type benchComplex struct {
	ID        int64        `db:"id"`
	Name      string       `db:"name"`
	Email     string       `db:"email"`
	Age       int          `db:"age"`
	Status    string       `db:"status"`
	Role      string       `db:"role"`
	CreatedAt time.Time    `db:"created_at"`
	UpdatedAt time.Time    `db:"updated_at"`
	Price     benchPricing `db:"price"`
	Mid       benchMiddle  `db:"mid"`
	Tags      []string     `db:"tags"`
}

// Simple — 5 flat fields, no VOs

func BenchmarkGetMappedCols_Simple_Cached(b *testing.B) {
	m := &benchSimple{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = GetMappedCols(m)
	}
}

func BenchmarkGetMappedCols_Simple_NoCache(b *testing.B) {
	m := &benchSimple{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = getMappedColsNoCache(m)
	}
}

// WithVO — 1 nested value object

func BenchmarkGetMappedCols_WithVO_Cached(b *testing.B) {
	m := &benchWithVO{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = GetMappedCols(m)
	}
}

func BenchmarkGetMappedCols_WithVO_NoCache(b *testing.B) {
	m := &benchWithVO{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = getMappedColsNoCache(m)
	}
}

// Complex — 11 fields, 2 VOs (one multi-level), 2 time.Time, 1 slice

func BenchmarkGetMappedCols_Complex_Cached(b *testing.B) {
	m := &benchComplex{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = GetMappedCols(m)
	}
}

func BenchmarkGetMappedCols_Complex_NoCache(b *testing.B) {
	m := &benchComplex{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = getMappedColsNoCache(m)
	}
}
