// Internal tests for rabbitmq/conn.go — accesses unexported types directly.
package rabbitmq

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/obs/logging"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	logging.NewLogger("rabbitmq-conn-internal-test")
}

// fakeConn implements amqpConn; drop simulates the broker closing it.
type fakeConn struct {
	mu         sync.Mutex
	notify     chan *amqp.Error
	closed     bool
	channelErr error
}

func (f *fakeConn) Channel() (*amqp.Channel, error) { return nil, f.channelErr }
func (f *fakeConn) NotifyClose(c chan *amqp.Error) chan *amqp.Error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notify = c
	return c
}
func (f *fakeConn) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.notify) // graceful close: channel closed without an error
	}
	return nil
}
func (f *fakeConn) IsClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}
func (f *fakeConn) drop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	f.notify <- &amqp.Error{Code: amqp.ConnectionForced, Reason: "broker restart"}
	close(f.notify)
}

// dialer hands out the given conns in order and then fails.
type dialer struct {
	mu    sync.Mutex
	conns []*fakeConn
	calls atomic.Int32
}

func (d *dialer) dial() (amqpConn, error) {
	d.calls.Add(1)
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.conns) == 0 {
		return nil, errors.New("connection refused")
	}
	c := d.conns[0]
	d.conns = d.conns[1:]
	return c, nil
}

func TestDialFailure(t *testing.T) {
	_, err := dial((&dialer{}).dial)
	assert.ErrorContains(t, err, "dial failed")
}

func TestConnChannelAndIsConnected(t *testing.T) {
	c, err := dial((&dialer{conns: []*fakeConn{{}}}).dial)
	require.NoError(t, err)
	defer c.Close()

	assert.True(t, c.IsConnected())
	_, err = c.channel()
	assert.NoError(t, err)
}

func TestConnChannelError(t *testing.T) {
	c, err := dial((&dialer{conns: []*fakeConn{{channelErr: errors.New("no channel")}}}).dial)
	require.NoError(t, err)
	defer c.Close()
	_, err = c.channel()
	assert.ErrorContains(t, err, "open channel failed")
}

func TestConnReconnectsAfterDrop(t *testing.T) {
	first, second := &fakeConn{}, &fakeConn{}
	d := &dialer{conns: []*fakeConn{first, second}}
	c, err := dial(d.dial)
	require.NoError(t, err)
	defer c.Close()

	first.drop()
	require.Eventually(t, func() bool {
		c.mu.RLock()
		defer c.mu.RUnlock()
		return c.conn == second
	}, 3*time.Second, 5*time.Millisecond)
	assert.True(t, c.IsConnected())
	assert.Equal(t, int32(2), d.calls.Load())
}

func TestConnNotConnectedWhileReconnecting(t *testing.T) {
	first := &fakeConn{}
	c, err := dial((&dialer{conns: []*fakeConn{first}}).dial)
	require.NoError(t, err)
	defer c.Close()

	first.drop()
	assert.False(t, c.IsConnected())
	require.Eventually(t, func() bool {
		_, err := c.channel()
		return errors.Is(err, ErrNotConnected)
	}, time.Second, time.Millisecond)
}

func TestConnCloseIsIdempotentAndStopsReconnecting(t *testing.T) {
	first := &fakeConn{}
	d := &dialer{conns: []*fakeConn{first}}
	c, err := dial(d.dial)
	require.NoError(t, err)

	require.NoError(t, c.Close())
	require.NoError(t, c.Close())
	assert.True(t, first.IsClosed())
	assert.False(t, c.IsConnected())
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(1), d.calls.Load(), "a graceful close must not reconnect")
}
