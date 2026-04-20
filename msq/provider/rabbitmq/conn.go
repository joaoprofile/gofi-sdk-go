// Package rabbitmq implements port.Broker for RabbitMQ using AMQP 0-9-1.
package rabbitmq

import (
	"context"
	"fmt"

	"github.com/joaoprofile/gofi/base/environment"
	"github.com/joaoprofile/gofi/base/observer"
	"github.com/joaoprofile/gofi/obs/logging"
	"github.com/rabbitmq/amqp091-go"
)

// amqpConn abstracts *amqp091.Connection to allow injection of test doubles.
type amqpConn interface {
	Channel() (*amqp091.Channel, error)
	Close() error
	IsClosed() bool
}

// Conn holds the AMQP connection and exposes lifecycle primitives.
type Conn struct {
	conn      amqpConn
	connected bool
}

type connObserver struct{ c *Conn }

func (o connObserver) Close() { o.c.Close() }

// Dial establishes a connection to RabbitMQ using environment variables.
func Dial() (*Conn, error) {
	env := environment.Instance()
	url := fmt.Sprintf("amqp://%s:%s@%s:%d/",
		env.MessagingUser,
		env.MessagingPassword,
		env.MessagingHost,
		env.MessagingPort,
	)
	return DialURL(url)
}

// DialURL establishes a connection to the given AMQP URL.
func DialURL(url string) (*Conn, error) {
	conn, err := amqp091.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq: dial failed: %w", err)
	}
	c := &Conn{conn: conn, connected: true}
	observer.Attach(connObserver{c})
	return c, nil
}

// Setup declares an AMQP exchange. Call once during application bootstrap.
func (c *Conn) Setup(_ context.Context, exchangeName string) error {
	ch, err := c.channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	if err := ch.ExchangeDeclare(
		exchangeName,
		amqp091.ExchangeDirect,
		true,  // durable
		false, // auto-delete
		false, // internal
		false, // no-wait
		nil,
	); err != nil {
		return fmt.Errorf("rabbitmq: exchange %q declare failed: %w", exchangeName, err)
	}
	return nil
}

func (c *Conn) Close() error {
	logging.Info("rabbitmq: closing connection")
	c.connected = false
	return c.conn.Close()
}

func (c *Conn) IsConnected() bool {
	return c.connected && !c.conn.IsClosed()
}

func (c *Conn) channel() (amqpChannel, error) {
	ch, err := c.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("rabbitmq: open channel failed: %w", err)
	}
	return ch, nil
}
