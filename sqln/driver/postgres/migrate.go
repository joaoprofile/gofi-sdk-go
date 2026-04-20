package postgres

import (
	"database/sql"

	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/joaoprofile/gofi/sqln/migrate"
)

type MigrateDriver struct{}

func (MigrateDriver) Name() string {
	return "postgres"
}

func (MigrateDriver) Instance(db *sql.DB) (database.Driver, error) {
	return postgres.WithInstance(db, &postgres.Config{})
}

func init() {
	migrate.RegisterDriver(MigrateDriver{})
}
