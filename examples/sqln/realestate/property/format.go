package property

import (
	"math"
	"strconv"
	"strings"
)

// formatNumber writes n the Brazilian way: 1.250.000 and 2,5.
func formatNumber(n float64) string {
	whole := int64(math.Floor(n))
	frac := int64(math.Round((n - float64(whole)) * 100))
	s := strconv.FormatInt(whole, 10)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	if frac > 0 {
		b.WriteString("," + strings.TrimSuffix(strconv.FormatInt(100+frac, 10)[1:], "0"))
	}
	return b.String()
}

func formatMoney(n float64) string { return "R$ " + formatNumber(math.Round(n)) }
