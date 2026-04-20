package connection

import (
	"database/sql"
	"log/slog"
	"time"
)

type PoolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	MaxConnLifeTime time.Duration
}

func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		MaxConnLifeTime: 5 * time.Minute,
	}
}

func applyPool(db *sql.DB, cfg PoolConfig) {
	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	}

	if cfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.MaxIdleConns)
	}

	if cfg.MaxConnLifeTime > 0 {
		db.SetConnMaxLifetime(cfg.MaxConnLifeTime)
	}
}

func startPoolMonitor(db *sql.DB, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)

		for range ticker.C {

			stats := db.Stats()

			slog.Debug(
				"sql pool stats",
				slog.Int("open", stats.OpenConnections),
				slog.Int("in_use", stats.InUse),
				slog.Int("idle", stats.Idle),
				slog.Int64("wait_count", stats.WaitCount),
				slog.Duration("wait_duration", stats.WaitDuration),
			)

		}

	}()
}
