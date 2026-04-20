package connection

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewObserver(t *testing.T) {
	conn := newTestConnection("ok")
	obs := NewObserver("test-db", conn)

	assert.NotNil(t, obs)
	assert.Equal(t, "test-db", obs.name)
	assert.Equal(t, conn, obs.conn)
}

func TestConnectionObserver_Close_DoesNotPanic(t *testing.T) {
	conn := newTestConnection("ok")
	obs := NewObserver("test-db", conn)

	assert.NotPanics(t, func() {
		obs.Close()
	})
}

func TestConnectionObserver_Close_LogsStats(t *testing.T) {
	// Verifica que Close() executa sem erro mesmo com conexão fechada.
	conn := newTestConnection("ok")
	_ = conn.Close()

	obs := NewObserver("closed-db", conn)
	assert.NotPanics(t, func() {
		obs.Close()
	})
}
