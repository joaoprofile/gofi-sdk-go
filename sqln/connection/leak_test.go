package connection

import (
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConnection_PingFailureClosesPool(t *testing.T) {
	registerFakeDriverOnce()
	before := connCloses.Load()

	_, err := NewConnection(Config{Driver: DriverName(testDriverDSN), DSN: "fail-ping"})
	require.Error(t, err)
	assert.Greater(t, connCloses.Load(), before, "the opened pool must be closed on failure")
}

func TestConnection_CloseStopsPoolMonitor(t *testing.T) {
	registerFakeDriverOnce()
	base := runtime.NumGoroutine()
	var conns []*Connection
	for range 20 {
		conns = append(conns, newTestConnection("ok"))
	}
	for _, c := range conns {
		require.NoError(t, c.Close())
	}
	assert.Eventually(t, func() bool { return runtime.NumGoroutine() <= base+5 },
		2*time.Second, 20*time.Millisecond, "pool monitor goroutines leaked")
}
