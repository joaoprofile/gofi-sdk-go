package connection

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

type sqlStateOnly struct{ code string }

func (e sqlStateOnly) Error() string    { return "driver error" }
func (e sqlStateOnly) SQLState() string { return e.code }

// Repositories classified driver errors with a type switch on one driver and
// silently missed the other: a permanent constraint error became "transient"
// and was retried forever. These helpers read the code regardless of wrapping.
func TestPgErrorHelpers(t *testing.T) {
	unique := WrapError("exec", fmt.Errorf("insert: %w", &pgconn.PgError{Code: "23505", ConstraintName: "uq_entity_email"}))
	check := &pgconn.PgError{Code: "23514", ConstraintName: "chk_window"}
	fk := fmt.Errorf("delete: %w", &pgconn.PgError{Code: "23503", ConstraintName: "fk_child_parent"})
	notNull := &pgconn.PgError{Code: "23502"}

	cases := []struct {
		name string
		got  bool
		want bool
	}{
		{"unique behind *Error", IsUniqueViolation(unique), true},
		{"unique on its constraint", IsUniqueViolation(unique, "uq_entity_email"), true},
		{"unique on another constraint", IsUniqueViolation(unique, "uq_other"), false},
		{"empty constraint means any", IsUniqueViolation(unique, ""), true},
		{"check", IsCheckViolation(check, "chk_window"), true},
		{"check is not unique", IsUniqueViolation(check), false},
		{"foreign key", IsForeignKeyViolation(fk, "fk_child_parent"), true},
		{"not null", IsNotNullViolation(notNull), true},
		{"nil", IsUniqueViolation(nil), false},
		{"not a postgres error", IsUniqueViolation(errors.New("boom")), false},
		{"other driver by SQLState()", IsUniqueViolation(fmt.Errorf("x: %w", sqlStateOnly{"23505"})), true},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}
	if got := SQLState(fk); got != "23503" {
		t.Errorf("SQLState = %q", got)
	}
	if got := ConstraintName(unique); got != "uq_entity_email" {
		t.Errorf("ConstraintName = %q", got)
	}
	if SQLState(errors.New("boom")) != "" || ConstraintName(nil) != "" {
		t.Error("non-postgres errors have no code or constraint")
	}
}
