package mysql

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/joaoprofile/gofi-sdk-go/sqln/connection"
	"github.com/joaoprofile/gofi-sdk-go/sqln/migrate"
)

func TestMigrationConfigEnablesMultiStatements(t *testing.T) {
	tests := map[string]string{
		"u:p@tcp(h:3306)/db":                       "u:p@tcp(h:3306)/db?multiStatements=true",
		"u:p@tcp(h:3306)/db?tls=true":              "u:p@tcp(h:3306)/db?tls=true&multiStatements=true",
		"u:p@tcp(h:3306)/db?":                      "u:p@tcp(h:3306)/db?multiStatements=true",
		"u:p@tcp(h:3306)/db?multiStatements=false": "u:p@tcp(h:3306)/db?multiStatements=false",
		"u:p?a/b@tcp(h:3306)/db?tls=false":         "u:p?a/b@tcp(h:3306)/db?tls=false&multiStatements=true",
		"u:p?multiStatements=x/@tcp(h:3306)/db":    "u:p?multiStatements=x/@tcp(h:3306)/db?multiStatements=true",
	}
	for in, want := range tests {
		got := Driver{}.MigrationConfig(connection.Config{DSN: in}).DSN
		assert.Equal(t, want, got, in)
	}
	_, ok := migrate.GetDriver("mysql")
	assert.True(t, ok, "importing the driver registers its migrations")
}
