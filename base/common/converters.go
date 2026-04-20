package common

import (
	"math"
	"strings"
	"unicode"
)

func CamelToSnake(s string) string {
	var builder strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 {
			builder.WriteRune('_')
		}
		builder.WriteRune(unicode.ToLower(r))
	}
	return builder.String()
}

func ToFixed(num float64, precision int) float64 {
	output := math.Pow(10, float64(precision))
	return float64(int(num*output)) / output
}

func Ptr(s string) *string {
	return &s
}
