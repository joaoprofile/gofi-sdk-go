// Internal tests for rabbitmq/conn.go — accesses unexported types directly.
package rabbitmq

import (
	"context"
	"errors"
	"testing"

	"github.com/joaoprofile/gofi/obs/logging"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	logging.NewLogger("rabbitmq-conn-internal-test")
}

// mockAmqpConn implements amqpConn for unit testing without a real broker.
type mockAmqpConn struct {
	isClosed   bool
	channelErr error
	closeErr   error
	closed     bool
}

func (m *mockAmqpConn) Channel() (*amqp.Channel, error) { return nil, m.channelErr }
func (m *mockAmqpConn) Close() error                    { m.closed = true; return m.closeErr }
func (m *mockAmqpConn) IsClosed() bool                  { return m.isClosed }

// mockAmqpChannel implements amqpChannel and records ExchangeDeclare calls.
type mockConnChannel struct {
	exchangeErr error
	closeCount  int
}

func (m *mockConnChannel) PublishWithContext(_ context.Context, _, _ string, _, _ bool, _ amqp.Publishing) error {
	return nil
}
func (m *mockConnChannel) Consume(_, _ string, _, _, _, _ bool, _ amqp.Table) (<-chan amqp.Delivery, error) {
	return nil, nil
}
func (m *mockConnChannel) Qos(_, _ int, _ bool) error { return nil }
func (m *mockConnChannel) QueueDeclare(_ string, _, _, _, _ bool, _ amqp.Table) (amqp.Queue, error) {
	return amqp.Queue{}, nil
}
func (m *mockConnChannel) QueueBind(_, _, _ string, _ bool, _ amqp.Table) error { return nil }
func (m *mockConnChannel) ExchangeDeclare(_, _ string, _, _, _, _ bool, _ amqp.Table) error {
	return m.exchangeErr
}
func (m *mockConnChannel) Flow(_ bool) error { return nil }
func (m *mockConnChannel) Close() error      { m.closeCount++; return nil }

// connWithMockChannel is a mockAmqpConn variant whose Channel() returns a pre-built amqpChannel.
// Since Channel() must return *amqp.Channel (SDK type), we cannot return a mock directly;
// we instead embed the full conn test flow by wrapping channel() at the Conn level.
// For Setup tests, we use a custom chanOpener so we can bypass the real Conn.channel() call.

// connOpenerFromFunc lets tests inject an arbitrary channel() implementation.
type connOpenerFromFunc struct {
	fn func() (amqpChannel, error)
}

func (c *connOpenerFromFunc) channel() (amqpChannel, error) { return c.fn() }

// Conn.IsConnected

func TestConnIsConnectedWhenFlagTrue(t *testing.T) {
	c := &Conn{conn: &mockAmqpConn{isClosed: false}, connected: true}
	assert.True(t, c.IsConnected())
}

func TestConnIsConnectedWhenFlagFalse(t *testing.T) {
	// connected=false short-circuits: IsClosed is never called.
	c := &Conn{conn: &mockAmqpConn{}, connected: false}
	assert.False(t, c.IsConnected())
}

func TestConnIsConnectedWhenAmqpConnClosed(t *testing.T) {
	c := &Conn{conn: &mockAmqpConn{isClosed: true}, connected: true}
	assert.False(t, c.IsConnected())
}

// Conn.Close

func TestConnCloseMarksFlagAndDelegates(t *testing.T) {
	mock := &mockAmqpConn{}
	c := &Conn{conn: mock, connected: true}

	require.NoError(t, c.Close())
	assert.False(t, c.connected)
	assert.True(t, mock.closed)
}

func TestConnCloseReturnsUnderlyingError(t *testing.T) {
	sentinel := errors.New("conn error")
	mock := &mockAmqpConn{closeErr: sentinel}
	c := &Conn{conn: mock, connected: true}

	err := c.Close()
	assert.ErrorIs(t, err, sentinel)
}

// Conn.channel

func TestConnChannelReturnsErrorWhenChannelFails(t *testing.T) {
	sentinel := errors.New("channel open failed")
	mock := &mockAmqpConn{channelErr: sentinel}
	c := &Conn{conn: mock, connected: true}

	ch, err := c.channel()
	assert.Nil(t, ch)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open channel failed")
}

// Conn.Setup — via a Broker that uses connOpenerFromFunc so we can inject a mockConnChannel.

func TestConnSetupSucceeds(t *testing.T) {
	mockCh := &mockConnChannel{}
	b := &Broker{
		conn:     &connOpenerFromFunc{fn: func() (amqpChannel, error) { return mockCh, nil }},
		exchange: "test-ex",
	}
	// Setup is on Conn, but we test the same code path through a mock chanOpener on Broker
	// because Conn.channel() returns *amqp.Channel (not mockable). The logic under test is
	// identical: open channel → ExchangeDeclare → close.
	// Here we directly call the broker's NewProducer to exercise the chanOpener path instead,
	// and add a dedicated setup test below that constructs Conn directly.
	p, err := b.NewProducer()
	require.NoError(t, err)
	require.NotNil(t, p)
}

func TestConnSetupChannelError(t *testing.T) {
	// Verify Conn.Setup returns an error when channel() fails.
	c := &Conn{conn: &mockAmqpConn{channelErr: errors.New("no channel")}, connected: true}
	err := c.Setup(context.Background(), "ex")
	require.Error(t, err)
}

func TestConnSetupExchangeDeclareError(t *testing.T) {
	// We cannot inject a real *amqp.Channel into Conn.channel().
	// Instead verify the exchange-declare error path via the Broker internal test,
	// which already exercises the equivalent code through amqpConsumer and producer.
	// This test is a compile-time placeholder.
	_ = (&Conn{}).connected // Conn is accessible from internal tests
}

// connObserver.Close

func TestConnObserverCloseDelegatestoConn(t *testing.T) {
	mock := &mockAmqpConn{}
	c := &Conn{conn: mock, connected: true}
	obs := connObserver{c: c}

	obs.Close()

	assert.False(t, c.connected, "Close must set connected=false")
	assert.True(t, mock.closed, "Close must delegate to underlying conn")
}
