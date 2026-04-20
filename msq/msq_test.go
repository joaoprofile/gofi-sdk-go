package msq_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/joaoprofile/gofi/msq"
	"github.com/joaoprofile/gofi/msq/core"
	"github.com/joaoprofile/gofi/msq/port"
	"github.com/joaoprofile/gofi/msq/provider/redis"
	"github.com/joaoprofile/gofi/msq/types"
	"github.com/joaoprofile/gofi/obs/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	logging.NewLogger("msq-test")
	os.Exit(m.Run())
}

// Type alias smoke tests

// Compile-time checks: the type aliases resolve to the expected underlying types.
var _ msq.Message = types.Message{}
var _ msq.ConsumeConfig = types.ConsumeConfig{}
var _ msq.Result = types.Result(0)
var _ msq.Producer = (port.Producer)(nil)
var _ msq.Consumer = (port.Consumer)(nil)
var _ msq.MessageHandler = (port.MessageHandler)(nil)
var _ msq.Broker = (port.Broker)(nil)
var _ msq.Messaging = (port.Broker)(nil)
var _ msq.MessagingBroker = (port.Broker)(nil)
var _ msq.ConsumerManager = core.ConsumerManager{}

// Result constants

func TestResultConstantsAreExported(t *testing.T) {
	assert.Equal(t, types.Ack, msq.Ack)
	assert.Equal(t, types.Nack, msq.Nack)
	assert.Equal(t, types.Ignore, msq.Ignore)
}

// NewMessage / NewMessageWithTopic

func TestNewMessageCreatesValidEnvelope(t *testing.T) {
	type payload struct{ Amount int }
	msg := msq.NewMessage(payload{Amount: 42})

	require.NotNil(t, msg)
	assert.NotEqual(t, [16]byte{}, msg.Id)
	assert.False(t, msg.Timestamp.IsZero())

	var got payload
	require.NoError(t, json.Unmarshal(msg.Value, &got))
	assert.Equal(t, 42, got.Amount)
}

func TestNewMessageWithTopicSetsTopicAndValue(t *testing.T) {
	msg := msq.NewMessageWithTopic("orders", "hello")

	assert.Equal(t, "orders", msg.Topic)
	assert.NotEmpty(t, msg.Value)
}

// UnpackMessage

func TestUnpackMessageRoundTrip(t *testing.T) {
	type order struct {
		ID    string
		Total float64
	}
	original := order{ID: "ord-1", Total: 99.9}
	msg := msq.NewMessage(original)

	got, err := msq.UnpackMessage[order](msg)
	require.NoError(t, err)
	assert.Equal(t, original, *got)
}

func TestUnpackMessageInvalidJSON(t *testing.T) {
	msg := &msq.Message{Value: json.RawMessage(`{broken`)}
	_, err := msq.UnpackMessage[struct{ X int }](msg)
	assert.Error(t, err)
}

// DefaultConsumeConfig

func TestDefaultConsumeConfigExported(t *testing.T) {
	cfg := msq.DefaultConsumeConfig("payments")

	assert.Equal(t, "payments", cfg.Topic)
	assert.Equal(t, msq.DefaultConcurrency, cfg.Concurrency)
	assert.Equal(t, msq.DefaultPollInterval, cfg.PollInterval)
}

// NewConsumerManager

func TestNewConsumerManagerNotNil(t *testing.T) {
	broker := &stubBroker{}
	mgr := msq.NewConsumerManager(broker)
	assert.NotNil(t, mgr)
}

// New

func TestNewWithNilBrokerReturnsError(t *testing.T) {
	svc, err := msq.New(msq.Config{Broker: nil})

	assert.Nil(t, svc)
	require.Error(t, err)
	assert.ErrorIs(t, err, core.ErrBrokerRequired)
}

func TestNewWithValidBrokerReturnsBrokerService(t *testing.T) {
	broker := &stubBroker{}
	svc, err := msq.New(msq.Config{Broker: broker})

	require.NoError(t, err)
	assert.NotNil(t, svc)
}

func TestNewWithOnEventCallback(t *testing.T) {
	var called bool
	broker := &stubBroker{}
	svc, err := msq.New(msq.Config{
		Broker:  broker,
		OnEvent: func(_ context.Context, _ msq.BrokerEvent) { called = true },
	})

	require.NoError(t, err)
	assert.NotNil(t, svc)
	_ = called // the callback is stored, not yet invoked
}

func TestNewBrokerServiceDelegates(t *testing.T) {
	broker := &stubBroker{}
	svc, err := msq.New(msq.Config{Broker: broker})
	require.NoError(t, err)

	// NewProducer and NewConsumer must delegate to the underlying broker.
	p := svc.NewProducer()
	assert.NotNil(t, p)

	c := svc.NewConsumer(msq.DefaultConsumeConfig("t"))
	assert.NotNil(t, c)
}

// msq.New with Redis broker (miniredis)

func TestNewWithRedisBrokerReturnsService(t *testing.T) {
	mr := miniredis.RunT(t)

	svc, err := msq.New(msq.Config{Broker: redis.New(redis.Config{Addr: mr.Addr()})})

	require.NoError(t, err)
	assert.NotNil(t, svc)
}

func TestNewRedisProducerWorks(t *testing.T) {
	mr := miniredis.RunT(t)
	svc, err := msq.New(msq.Config{Broker: redis.New(redis.Config{Addr: mr.Addr()})})
	require.NoError(t, err)

	producer := svc.NewProducer()
	require.NotNil(t, producer)
	defer producer.Close()

	// Publishing to a topic with no subscriber is valid for Redis Pub/Sub.
	msg := msq.NewMessageWithTopic("test", "hello")
	assert.NoError(t, producer.SendMessage(context.Background(), msg))
}

func TestNewConsumerManagerFromService(t *testing.T) {
	mr := miniredis.RunT(t)
	svc, err := msq.New(msq.Config{Broker: redis.New(redis.Config{Addr: mr.Addr()})})
	require.NoError(t, err)

	mgr := svc.NewConsumerManager()
	assert.NotNil(t, mgr)
}

// Stubs

type stubProducer struct{}

func (s *stubProducer) SendMessage(_ context.Context, _ *types.Message) error         { return nil }
func (s *stubProducer) SendMessagesBatch(_ context.Context, _ []*types.Message) error { return nil }
func (s *stubProducer) Close() error                                                  { return nil }

type stubConsumer struct{}

func (s *stubConsumer) Consume(_ context.Context, _ port.MessageHandler) error { return nil }
func (s *stubConsumer) Close() error                                           { return nil }
func (s *stubConsumer) Pause() error                                           { return nil }
func (s *stubConsumer) Resume() error                                          { return nil }

type stubBroker struct{}

func (b *stubBroker) NewProducer() port.Producer                      { return &stubProducer{} }
func (b *stubBroker) NewConsumer(_ types.ConsumeConfig) port.Consumer { return &stubConsumer{} }
