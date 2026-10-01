package pagination

import "testing"

func TestGetOrder_RejectsInjection(t *testing.T) {
	tests := []struct {
		name  string
		order []Sort
		want  string
	}{
		{"valid", []Sort{{"created_at", DESC}, {"u.name", ASC}}, "created_at DESC, u.name ASC"},
		{"lowercase direction", []Sort{{"id", "desc"}}, "id DESC"},
		{"injected direction", []Sort{{"id", "ASC, (SELECT pg_sleep(10))"}}, "id ASC"},
		{"injected field", []Sort{{"id; DROP TABLE users", ASC}, {"name", DESC}}, "name DESC"},
		{"quoted identifier", []Sort{{`"createdAt"`, DESC}}, `"createdAt" DESC`},
		{"nulls last", []Sort{{"score", "desc nulls last"}}, "score DESC NULLS LAST"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewPageRequest(0, 10, tt.order).GetOrder(); got != tt.want {
				t.Errorf("GetOrder()=%q, want %q", got, tt.want)
			}
		})
	}
}

// A MySQL "…" string literal closed with \" must not smuggle a blind-SQLi subquery into ORDER BY.
func TestGetOrder_DropsMySQLQuotedPayload(t *testing.T) {
	payload := `"a\".", (SELECT IF(SUBSTR(password,1,1)='a',SLEEP(1),0) FROM users LIMIT 1) -- "`
	for _, pr := range []*PageRequest{
		NewPageRequest(0, 10, []Sort{NewSort(payload, ASC)}),
		NewPageRequestFromParams(0, 10, payload, "ASC"),
	} {
		if got := pr.GetOrder(); got != "" {
			t.Errorf("GetOrder()=%q, want the field dropped", got)
		}
	}
}
