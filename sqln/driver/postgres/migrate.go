package postgres

import (
	"database/sql"

	"github.com/gofi-labs/gofi-sdk-go/sqln/migrate"
	"github.com/golang-migrate/migrate/v4/database"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
)

type MigrateDriver struct{}

func (MigrateDriver) Name() string {
	return "postgres"
}

func (MigrateDriver) Instance(db *sql.DB) (database.Driver, error) {
	return pgxmigrate.WithInstance(db, &pgxmigrate.Config{})
}

func init() {
	migrate.RegisterDriver(MigrateDriver{})
}
