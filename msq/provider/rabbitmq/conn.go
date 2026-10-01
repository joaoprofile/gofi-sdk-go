// Package rabbitmq implements port.Broker for RabbitMQ using AMQP 0-9-1.
package rabbitmq

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/msq/worker"
	"github.com/gofi-labs/gofi-sdk-go/obs/logging"
	"github.com/rabbitmq/amqp091-go"
)

// ErrNotConnected is returned while the connection is being re-established.
var ErrNotConnected = errors.New("rabbitmq: not connected")

// amqpConn abstracts *amqp091.Connection to allow injection of test doubles.
type amqpConn interface {
	Channel() (*amqp091.Channel, error)
	NotifyClose(receiver chan *amqp091.Error) chan *amqp091.Error
	Close() error
	IsClosed() bool
}

// Conn is an AMQP connection that re-dials with backoff when the broker drops
// it. Producers and consumers re-open their channels on the new connection.
type Conn struct {
	dial func() (amqpConn, error)

	mu     sync.RWMutex
	conn   amqpConn // nil while reconnecting
	closed bool

	done      chan struct{} // closed by Close; stops reconnecting
	closeOnce sync.Once
	connected atomic.Bool
}

// DialOption configures DialURL.
type DialOption func(*amqp091.Config)

// WithTLSConfig sets the TLS configuration of an amqps:// connection
// (private CA, client certificate, server name). Versions below TLS 1.2 are
// raised to it.
func WithTLSConfig(c *tls.Config) DialOption {
	return func(cfg *amqp091.Config) {
		if c == nil {
			return
		}
		t := c.Clone()
		t.MinVersion = max(t.MinVersion, tls.VersionTLS12)
		cfg.TLSClientConfig = t
	}
}

// DialURL establishes a connection to the given AMQP URL (amqp:// or amqps://).
// Credentials in the URL are sent with SASL PLAIN and never appear in errors
// or logs. The caller closes it (Broker.Close does; gofi's builder closes the
// Broker).
func DialURL(rawURL string, opts ...DialOption) (*Conn, error) {
	target, cfg, err := dialTarget(rawURL, opts)
	if err != nil {
		return nil, err
	}
	return dial(func() (amqpConn, error) {
		c := cfg
		if cfg.TLSClientConfig != nil {
			c.TLSClientConfig = cfg.TLSClientConfig.Clone() // amqp091 sets ServerName on it
		}
		return amqp091.DialConfig(target, c)
	})
}

// dialTarget splits the credentials out of rawURL: the returned URL has none,
// so no amqp091 error can echo them.
func dialTarget(rawURL string, opts []DialOption) (string, amqp091.Config, error) {
	var cfg amqp091.Config
	u, err := url.Parse(rawURL)
	if err != nil {
		// url.Error repeats the whole URL, password included.
		if ue, ok := errors.AsType[*url.Error](err); ok {
			err = ue.Err
		}
		return "", cfg, fmt.Errorf("rabbitmq: invalid AMQP URL: %w", err)
	}
	if u.Scheme != "amqp" && u.Scheme != "amqps" {
		return "", cfg, fmt.Errorf("rabbitmq: invalid AMQP URL scheme %q (amqp or amqps)", u.Scheme)
	}
	if u.Hostname() == "" {
		return "", cfg, errors.New("rabbitmq: invalid AMQP URL: missing host")
	}
	if u.User != nil {
		password, _ := u.User.Password()
		cfg.SASL = []amqp091.Authentication{&amqp091.PlainAuth{Username: u.User.Username(), Password: password}}
		u.User = nil
	}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.TLSClientConfig != nil && u.Scheme != "amqps" {
		return "", cfg, errors.New("rabbitmq: a TLS config needs an amqps:// URL")
	}
	return u.String(), cfg, nil
}

func dial(d func() (amqpConn, error)) (*Conn, error) {
	conn, err := d()
	if err != nil {
		return nil, fmt.Errorf("rabbitmq: dial failed: %w", err)
	}
	c := &Conn{dial: d, done: make(chan struct{})}
	c.attach(conn)
	return c, nil
}

// attach installs conn and watches it; it reports false when Close won the race.
func (c *Conn) attach(conn amqpConn) bool {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = conn.Close()
		return false
	}
	c.conn = conn
	c.connected.Store(true)
	c.mu.Unlock()
	go c.watch(conn.NotifyClose(make(chan *amqp091.Error, 1)))
	return true
}

// watch re-dials after an unexpected close. A graceful close (Close) closes
// the notification channel without an error.
func (c *Conn) watch(lost chan *amqp091.Error) {
	amqpErr, ok := <-lost
	if !ok || amqpErr == nil {
		return
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.conn = nil
	c.connected.Store(false)
	c.mu.Unlock()
	logging.Error("rabbitmq: connection lost, reconnecting", slog.Any("error", amqpErr))

	backoff := worker.Backoff{Min: worker.ReceiveBackoffMin, Max: worker.ReceiveBackoffMax}
	for {
		t := time.NewTimer(backoff.Next())
		select {
		case <-c.done:
			t.Stop()
			return
		case <-t.C:
		}
		conn, err := c.dial()
		if err != nil {
			logging.Warn("rabbitmq: reconnect failed", slog.Any("error", err))
			continue
		}
		if c.attach(conn) {
			logging.Info("rabbitmq: reconnected")
		}
		return
	}
}

// Setup declares a durable direct exchange. Broker.Setup calls it; call it
// directly only when using Conn without a Broker.
func (c *Conn) Setup(_ context.Context, exchangeName string) error {
	return declareExchange(c, exchangeName)
}

// declareExchange keeps an existing exchange as it is (any kind) and creates
// a durable direct one when it is missing.
func declareExchange(o chanOpener, name string) error {
	ch, err := o.channel()
	if err != nil {
		return err
	}
	err = ch.ExchangeDeclarePassive(name, amqp091.ExchangeDirect, true, false, false, false, nil)
	ch.Close()
	if amqpErr, ok := errors.AsType[*amqp091.Error](err); err == nil || !ok || amqpErr.Code != amqp091.NotFound {
		if err != nil {
			return fmt.Errorf("rabbitmq: exchange %q check failed: %w", name, err)
		}
		return nil
	}
	// A failed passive declare closes the channel: declare on a new one.
	if ch, err = o.channel(); err != nil {
		return err
	}
	defer ch.Close()
	if err := ch.ExchangeDeclare(name, amqp091.ExchangeDirect, true, false, false, false, nil); err != nil {
		return fmt.Errorf("rabbitmq: exchange %q declare failed: %w", name, err)
	}
	return nil
}

// Close closes the connection and stops reconnecting. It is idempotent.
func (c *Conn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		logging.Info("rabbitmq: closing connection")
		c.mu.Lock()
		c.closed = true
		conn := c.conn
		c.conn = nil
		c.connected.Store(false)
		c.mu.Unlock()
		close(c.done)
		if conn != nil {
			err = conn.Close()
		}
	})
	return err
}

// IsConnected reports whether a live connection is available.
func (c *Conn) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected.Load() && c.conn != nil && !c.conn.IsClosed()
}

func (c *Conn) channel() (amqpChannel, error) {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()
	if conn == nil {
		return nil, ErrNotConnected
	}
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("rabbitmq: open channel failed: %w", err)
	}
	return &realChannel{Channel: ch}, nil
}

// realChannel adapts *amqp091.Channel to amqpChannel.
type realChannel struct {
	*amqp091.Channel
	returns *returnTracker // set by Confirm
}

// Confirm puts the channel in confirm mode and starts tracking basic.return,
// so a publish no queue receives fails instead of being confirmed.
func (r *realChannel) Confirm(noWait bool) error {
	if err := r.Channel.Confirm(noWait); err != nil {
		return err
	}
	r.returns = newReturnTracker(r.NotifyReturn(make(chan amqp091.Return, 64)))
	return nil
}

// Publish sends msg as mandatory and returns a wait func that blocks until the
// broker confirms it. A message returned as unroutable fails with
// ErrUnroutable even though the broker then acks it.
func (r *realChannel) Publish(ctx context.Context, exchange, key string, msg amqp091.Publishing) (func(context.Context) error, error) {
	var p *pendingPublish
	if r.returns != nil {
		p = r.returns.track(msg.MessageId)
	}
	conf, err := r.PublishWithDeferredConfirmWithContext(ctx, exchange, key, true, false, msg)
	if err != nil {
		r.returns.untrack(p)
		return nil, err
	}
	return func(ctx context.Context) error {
		defer r.returns.untrack(p)
		if conf == nil {
			return nil
		}
		acked, err := conf.WaitContext(ctx)
		if err != nil {
			return err
		}
		if !acked {
			return errors.New("rabbitmq: publish nacked by broker")
		}
		return r.returns.result(p)
	}, nil
}

// ErrUnroutable is returned when the broker has no queue bound for the
// message's routing key: the message was not stored.
var ErrUnroutable = errors.New("rabbitmq: message unroutable")

// pendingPublish is a publish waiting for its confirm; ret is set when the
// broker returned it.
type pendingPublish struct {
	id  string
	ret *amqp091.Return
}

// returnTracker matches basic.return frames to publishes by MessageId.
// amqp091 hands a Return to the listener before it dispatches the ack of the
// same publish, so once the ack is seen, a sync drains any Return that is
// buffered or being matched.
type returnTracker struct {
	mu      sync.Mutex
	pending map[string][]*pendingPublish

	syncReq chan chan struct{}
	stopped chan struct{} // closed when the channel closes
}

func newReturnTracker(returns <-chan amqp091.Return) *returnTracker {
	t := &returnTracker{
		pending: make(map[string][]*pendingPublish),
		syncReq: make(chan chan struct{}),
		stopped: make(chan struct{}),
	}
	go t.run(returns)
	return t
}

func (t *returnTracker) run(returns <-chan amqp091.Return) {
	defer close(t.stopped)
	for {
		select {
		case ret, ok := <-returns:
			if !ok {
				return
			}
			t.match(ret)
		case done := <-t.syncReq:
			ok := t.drain(returns)
			close(done)
			if !ok {
				return
			}
		}
	}
}

// drain matches every buffered Return; it reports false once returns closed.
func (t *returnTracker) drain(returns <-chan amqp091.Return) bool {
	for {
		select {
		case ret, ok := <-returns:
			if !ok {
				return false
			}
			t.match(ret)
		default:
			return true
		}
	}
}

// match marks the oldest unreturned publish with the Return's MessageId.
func (t *returnTracker) match(ret amqp091.Return) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, p := range t.pending[ret.MessageId] {
		if p.ret == nil {
			p.ret = &ret
			return
		}
	}
}

func (t *returnTracker) track(id string) *pendingPublish {
	p := &pendingPublish{id: id}
	t.mu.Lock()
	t.pending[id] = append(t.pending[id], p)
	t.mu.Unlock()
	return p
}

func (t *returnTracker) untrack(p *pendingPublish) {
	if t == nil || p == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	list := t.pending[p.id]
	for i, q := range list {
		if q == p {
			list = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(list) == 0 {
		delete(t.pending, p.id)
	} else {
		t.pending[p.id] = list
	}
}

// result is called after the publish was acked: it waits until every Return
// sent before that ack is matched, then reports whether p was returned.
func (t *returnTracker) result(p *pendingPublish) error {
	if t == nil || p == nil {
		return nil
	}
	done := make(chan struct{})
	select {
	case t.syncReq <- done:
		<-done
	case <-t.stopped:
	}
	t.mu.Lock()
	ret := p.ret
	t.mu.Unlock()
	if ret == nil {
		return nil
	}
	return fmt.Errorf("%w: exchange %q, routing key %q: %d %s", ErrUnroutable, ret.Exchange, ret.RoutingKey, ret.ReplyCode, ret.ReplyText)
}
