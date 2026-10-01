package connection

import (
	"database/sql"
	"log/slog"
	"sync"
	"time"
)

// PoolConfig bounds the pool. A zero field takes its DefaultPoolConfig value,
// so a zero PoolConfig never means database/sql's unlimited connections and
// infinite lifetime. A negative MaxOpenConns or MaxConnLifeTime explicitly
// removes that limit; a negative MaxIdleConns keeps no idle connections.
type PoolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	MaxConnLifeTime time.Duration
	// MaxIdleTime closes connections idle for longer, returning them to the
	// database between bursts (0 keeps them until MaxConnLifeTime).
	MaxIdleTime time.Duration
}

func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		MaxConnLifeTime: 5 * time.Minute,
	}
}

// withDefaults fills the zero fields of cfg from DefaultPoolConfig.
func (cfg PoolConfig) withDefaults() PoolConfig {
	def := DefaultPoolConfig()
	if cfg.MaxOpenConns == 0 {
		cfg.MaxOpenConns = def.MaxOpenConns
	}
	if cfg.MaxIdleConns == 0 {
		cfg.MaxIdleConns = def.MaxIdleConns
	}
	if cfg.MaxConnLifeTime == 0 {
		cfg.MaxConnLifeTime = def.MaxConnLifeTime
	}
	return cfg
}

func applyPool(db *sql.DB, cfg PoolConfig) {
	cfg = cfg.withDefaults()
	// database/sql treats open <= 0 as unlimited, idle <= 0 as none and
	// lifetime <= 0 as forever.
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.MaxConnLifeTime)
	if cfg.MaxIdleTime > 0 {
		db.SetConnMaxIdleTime(cfg.MaxIdleTime)
	}
}

// startPoolMonitor logs pool stats periodically until the returned stop is called.
func startPoolMonitor(db *sql.DB, interval time.Duration) (stop func()) {
	done := make(chan struct{})
	var once sync.Once
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
			}

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
	return func() { once.Do(func() { close(done) }) }
}
