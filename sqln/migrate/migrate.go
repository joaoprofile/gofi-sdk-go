package migrate

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
)

// Run applies migrations using db; the caller keeps ownership of db.
func Run(db *sql.DB, driverName string, cfg Config) error {
	return run(db, driverName, cfg, false)
}

// RunAndClose applies migrations on a database opened only for them and closes it
// afterwards, releasing the connection golang-migrate keeps pinned.
func RunAndClose(db *sql.DB, driverName string, cfg Config) error {
	return run(db, driverName, cfg, true)
}

func run(db *sql.DB, driverName string, cfg Config, closeAfter bool) error {
	driver, ok := getDriver(driverName)
	if !ok {
		return fmt.Errorf("migration driver not registered: %s", driverName)
	}

	instance, err := driver.Instance(db)
	if err != nil {
		if closeAfter {
			_ = db.Close()
		}
		return err
	}
	if closeAfter {
		defer instance.Close()
	}

	if cfg.FS != (embed.FS{}) {
		if _, err := fs.Stat(cfg.FS, cfg.Path); err == nil {
			return runEmbedded(db, driverName, cfg, instance)
		}
		// Keeps setups that set FS but whose Path only exists on disk working.
		slog.Warn("sqln/migrate: path not found in embedded FS, using filesystem", slog.String("path", cfg.Path))
	}

	return runFilesystem(driverName, cfg, instance)
}
