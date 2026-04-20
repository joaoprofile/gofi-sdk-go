package criteria

// Internal test — toSlice is unexported; tested directly here.

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Recognised slice types

func TestToSlice_AnySlice_Passthrough(t *testing.T) {
	input := []any{1, "two", 3.0}
	out, ok := toSlice(input)
	assert.True(t, ok)
	assert.Equal(t, input, out)
}

func TestToSlice_AnySlice_EmptySlice(t *testing.T) {
	out, ok := toSlice([]any{})
	assert.True(t, ok)
	assert.Equal(t, []any{}, out)
}

func TestToSlice_StringSlice_ConvertsToAny(t *testing.T) {
	out, ok := toSlice([]string{"a", "b", "c"})
	assert.True(t, ok)
	assert.Equal(t, []any{"a", "b", "c"}, out)
}

func TestToSlice_StringSlice_Empty(t *testing.T) {
	out, ok := toSlice([]string{})
	assert.True(t, ok)
	assert.Equal(t, []any{}, out)
}

func TestToSlice_IntSlice_ConvertsToAny(t *testing.T) {
	out, ok := toSlice([]int{1, 2, 3})
	assert.True(t, ok)
	assert.Equal(t, []any{1, 2, 3}, out)
}

func TestToSlice_IntSlice_PreservesValues(t *testing.T) {
	out, ok := toSlice([]int{-1, 0, 1})
	assert.True(t, ok)
	assert.Equal(t, []any{-1, 0, 1}, out)
}

func TestToSlice_Int32Slice_ConvertsToAny(t *testing.T) {
	out, ok := toSlice([]int32{10, 20, 30})
	assert.True(t, ok)
	assert.Equal(t, []any{int32(10), int32(20), int32(30)}, out)
}

func TestToSlice_Int32Slice_Empty(t *testing.T) {
	out, ok := toSlice([]int32{})
	assert.True(t, ok)
	assert.Equal(t, []any{}, out)
}

func TestToSlice_Int64Slice_ConvertsToAny(t *testing.T) {
	out, ok := toSlice([]int64{100, 200})
	assert.True(t, ok)
	assert.Equal(t, []any{int64(100), int64(200)}, out)
}

func TestToSlice_Int64Slice_PreservesLargeValues(t *testing.T) {
	large := int64(1 << 62)
	out, ok := toSlice([]int64{large})
	assert.True(t, ok)
	assert.Equal(t, []any{large}, out)
}

func TestToSlice_Float64Slice_ConvertsToAny(t *testing.T) {
	out, ok := toSlice([]float64{1.1, 2.2, 3.3})
	assert.True(t, ok)
	assert.Equal(t, []any{1.1, 2.2, 3.3}, out)
}

func TestToSlice_Float64Slice_PreservesDecimals(t *testing.T) {
	out, ok := toSlice([]float64{0.001, -0.001})
	assert.True(t, ok)
	assert.Equal(t, []any{0.001, -0.001}, out)
}

// Unrecognised types -> (nil, false)

func TestToSlice_String_ReturnsFalse(t *testing.T) {
	out, ok := toSlice("hello")
	assert.False(t, ok)
	assert.Nil(t, out)
}

func TestToSlice_Int_ReturnsFalse(t *testing.T) {
	out, ok := toSlice(42)
	assert.False(t, ok)
	assert.Nil(t, out)
}

func TestToSlice_Float_ReturnsFalse(t *testing.T) {
	out, ok := toSlice(3.14)
	assert.False(t, ok)
	assert.Nil(t, out)
}

func TestToSlice_Bool_ReturnsFalse(t *testing.T) {
	out, ok := toSlice(true)
	assert.False(t, ok)
	assert.Nil(t, out)
}

func TestToSlice_Nil_ReturnsFalse(t *testing.T) {
	out, ok := toSlice(nil)
	assert.False(t, ok)
	assert.Nil(t, out)
}

func TestToSlice_Struct_ReturnsFalse(t *testing.T) {
	type point struct{ x, y int }
	out, ok := toSlice(point{1, 2})
	assert.False(t, ok)
	assert.Nil(t, out)
}

// []int8 and []uint are not handled — confirm they return false.
func TestToSlice_Int8Slice_ReturnsFalse(t *testing.T) {
	out, ok := toSlice([]int8{1, 2, 3})
	assert.False(t, ok)
	assert.Nil(t, out)
}

func TestToSlice_UintSlice_ReturnsFalse(t *testing.T) {
	out, ok := toSlice([]uint{1, 2, 3})
	assert.False(t, ok)
	assert.Nil(t, out)
}

// Output is always a fresh slice (no aliasing)

func TestToSlice_StringSlice_OutputIsNotAliased(t *testing.T) {
	input := []string{"a", "b"}
	out, _ := toSlice(input)
	// Mutate input; output must not change.
	input[0] = "z"
	assert.Equal(t, "a", out[0])
}

func TestToSlice_IntSlice_OutputIsNotAliased(t *testing.T) {
	input := []int{1, 2}
	out, _ := toSlice(input)
	input[0] = 99
	assert.Equal(t, 1, out[0])
}

// []any is returned as-is (same backing array) — this is intentional and documented.
func TestToSlice_AnySlice_SameReference(t *testing.T) {
	input := []any{"x"}
	out, _ := toSlice(input)
	assert.Same(t, &input[0], &out[0])
}

// Single-element slices

func TestToSlice_SingleString(t *testing.T) {
	out, ok := toSlice([]string{"only"})
	assert.True(t, ok)
	assert.Equal(t, []any{"only"}, out)
}

func TestToSlice_SingleInt64(t *testing.T) {
	out, ok := toSlice([]int64{99})
	assert.True(t, ok)
	assert.Equal(t, []any{int64(99)}, out)
}
