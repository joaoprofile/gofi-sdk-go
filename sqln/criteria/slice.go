package criteria

import "reflect"

// toSlice normalises any slice (except []byte, a scalar) into []any; common
// types skip reflection. Returns (nil, false) for non-slices.
func toSlice(value any) ([]any, bool) {
	switch v := value.(type) {

	case []any:
		return v, true

	case []string:
		out := make([]any, len(v))
		for i := range v {
			out[i] = v[i]
		}
		return out, true

	case []int:
		out := make([]any, len(v))
		for i := range v {
			out[i] = v[i]
		}
		return out, true

	case []int32:
		out := make([]any, len(v))
		for i := range v {
			out[i] = v[i]
		}
		return out, true

	case []int64:
		out := make([]any, len(v))
		for i := range v {
			out[i] = v[i]
		}
		return out, true

	case []float64:
		out := make([]any, len(v))
		for i := range v {
			out[i] = v[i]
		}
		return out, true
	}

	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice || rv.Type().Elem().Kind() == reflect.Uint8 {
		return nil, false
	}
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}
