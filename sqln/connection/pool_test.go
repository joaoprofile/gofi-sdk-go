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

func TestApplyPool_ZeroValuesSkipped(t *testing.T) {
	db := mustOpenTestDB("ok")
	defer db.Close()

	// Aplicar valores iniciais
	applyPool(db, PoolConfig{MaxOpenConns: 10, MaxIdleConns: 5, MaxConnLifeTime: time.Minute})

	// Aplicar config com zeros — não deve alterar
	applyPool(db, PoolConfig{MaxOpenConns: 0, MaxIdleConns: 0, MaxConnLifeTime: 0})

	stats := db.Stats()
	assert.Equal(t, 10, stats.MaxOpenConnections)
}

func TestStartPoolMonitor_DoesNotPanic(t *testing.T) {
	db := mustOpenTestDB("ok")
	defer db.Close()

	assert.NotPanics(t, func() {
		startPoolMonitor(db, 100*time.Millisecond)
	})
}
