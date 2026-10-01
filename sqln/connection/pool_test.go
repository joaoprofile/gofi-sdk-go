package connection

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultPoolConfig(t *testing.T) {
	cfg := DefaultPoolConfig()

	assert.Equal(t, 10, cfg.MaxOpenConns)
	assert.Equal(t, 5, cfg.MaxIdleConns)
	assert.Equal(t, 5*time.Minute, cfg.MaxConnLifeTime)
}

func TestApplyPool_AllValuesSet(t *testing.T) {
	db := mustOpenTestDB("ok")
	defer db.Close()

	cfg := PoolConfig{
		MaxOpenConns:    20,
		MaxIdleConns:    10,
		MaxConnLifeTime: 2 * time.Minute,
	}
	applyPool(db, cfg)

	stats := db.Stats()
	assert.Equal(t, 20, stats.MaxOpenConnections)
}

// Regression: a zero PoolConfig left database/sql's unlimited connections.
func TestApplyPool_ZeroValuesUseDefaults(t *testing.T) {
	db := mustOpenTestDB("ok")
	defer db.Close()

	applyPool(db, PoolConfig{})

	assert.Equal(t, DefaultPoolConfig().MaxOpenConns, db.Stats().MaxOpenConnections)
	assert.Equal(t, PoolConfig{MaxOpenConns: 10, MaxIdleConns: 5, MaxConnLifeTime: 5 * time.Minute}, PoolConfig{}.withDefaults())
	assert.Equal(t, PoolConfig{MaxOpenConns: 50, MaxIdleConns: 5, MaxConnLifeTime: 5 * time.Minute}, PoolConfig{MaxOpenConns: 50}.withDefaults())
}

func TestApplyPool_NegativeRemovesLimit(t *testing.T) {
	db := mustOpenTestDB("ok")
	defer db.Close()

	applyPool(db, PoolConfig{MaxOpenConns: -1, MaxConnLifeTime: -1})

	assert.Equal(t, 0, db.Stats().MaxOpenConnections, "0 is database/sql's unlimited")
}

func TestStartPoolMonitor_DoesNotPanic(t *testing.T) {
	db := mustOpenTestDB("ok")
	defer db.Close()

	assert.NotPanics(t, func() {
		startPoolMonitor(db, 100*time.Millisecond)
	})
}
