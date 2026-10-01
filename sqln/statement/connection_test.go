package statement

import (
	"context"
	"testing"

	"github.com/gofi-labs/gofi-sdk-go/sqln/connection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A statement bound to a connection works without any global connection.
func TestNewWithConnection(t *testing.T) {
	initDriver()
	connection.ResetGlobalForTest()
	c, err := connection.NewConnection(connection.Config{Driver: connection.DriverName(testDriver), DSN: "ok"})
	require.NoError(t, err)
	defer c.Close()

	st := NewWithConnection(c)
	_, err = st.Execute(context.Background(), "UPDATE t SET x = 1")
	assert.NoError(t, err)
	_, err = st.Execute(context.Background(), "")
	assert.Error(t, err)
}
