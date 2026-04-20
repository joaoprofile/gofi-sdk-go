package criteria

// toSlice normalises the common typed-slice variants into []any.
// Returns (nil, false) when value is not a recognised slice type.
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

	return nil, false
}
