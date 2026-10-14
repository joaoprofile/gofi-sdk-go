package mysql

import (
	"database/sql"
	"strings"

	"github.com/golang-migrate/migrate/v4/database"
	gomysql "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/joaoprofile/gofi-sdk-go/sqln/connection"
	"github.com/joaoprofile/gofi-sdk-go/sqln/migrate"
)

type MigrateDriver struct{}

func (MigrateDriver) Name() string {
	return "mysql"
}

// Instance needs a pool opened with multiStatements=true, as
// Driver.MigrationConfig sets, to run files holding several statements.
func (MigrateDriver) Instance(db *sql.DB) (database.Driver, error) {
	return gomysql.WithInstance(db, &gomysql.Config{})
}

// MigrationConfig enables multiStatements on the migration pool only: the
// query pools keep it off, so an injected "; …" can never run a second
// statement there.
func (Driver) MigrationConfig(cfg connection.Config) connection.Config {
	cfg.DSN = withParam(cfg.DSN, "multiStatements", "true")
	return cfg
}

// withParam adds key=value to the parameters of a go-sql-driver DSN, which
// follow the last '/' (the password may hold '/' and '?'), unless key is set.
func withParam(dsn, key, value string) string {
	params := dsn[strings.LastIndexByte(dsn, '/')+1:]
	_, query, ok := strings.Cut(params, "?")
	if !ok {
		return dsn + "?" + key + "=" + value
	}
	for _, kv := range strings.Split(query, "&") {
		if k, _, _ := strings.Cut(kv, "="); k == key {
			return dsn
		}
	}
	if query == "" {
		return dsn + key + "=" + value
	}
	return dsn + "&" + key + "=" + value
}

func init() {
	migrate.RegisterDriver(MigrateDriver{})
}
