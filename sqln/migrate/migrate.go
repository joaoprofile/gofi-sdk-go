package migrate

import (
	"database/sql"
	"fmt"
)

func Run(db *sql.DB, driverName string, cfg Config) error {

	driver, ok := getDriver(driverName)
	if !ok {
		return fmt.Errorf("migration driver not registered: %s", driverName)
	}

	instance, err := driver.Instance(db)
	if err != nil {
		return err
	}

	if cfg.FS != (cfg.FS) {
		return runEmbedded(db, driverName, cfg, instance)
	}

	return runFilesystem(driverName, cfg, instance)
}
