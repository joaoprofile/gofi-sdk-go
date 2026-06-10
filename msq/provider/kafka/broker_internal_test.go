// Internal tests for the kafka package — access unexported types directly.
package kafka

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/joaoprofile/gofi/msq/port"
	"github.com/joaoprofile/gofi/msq/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock: sarama.SyncProducer

type mockSyncProducer struct {
	sendErr     error
	batchErr    error
	lastMessage *sarama.ProducerMessage
	lastBatch   []*sarama.ProducerMessage
}

func (m *mockSyncProducer) SendMessage(msg *sarama.ProducerMessage) (int32, int64, error) {
	m.lastMessage = msg
	return 0, 0, m.sendErr
}
func (m *mockSyncProducer) SendMessages(msgs []*sarama.ProducerMessage) error {
	m.lastBatch = msgs
	return m.batchErr
}
func (m *mockSyncProducer) Close() error                            { return nil }
func (m *mockSyncProducer) TxnStatus() sarama.ProducerTxnStatusFlag { return 0 }
func (m *mockSyncProducer) IsTransactional() bool                   { return false }
func (m *mockSyncProducer) BeginTxn() error                         { return nil }
func (m *mockSyncProducer) CommitTxn() error                        { return nil }
func (m *mockSyncProducer) AbortTxn() error                         { return nil }
func (m *mockSyncProducer) AddOffsetsToTxn(_ map[string][]*sarama.PartitionOffsetMetadata, _ string) error {
	return nil
}
func (m *mockSyncProducer) AddMessageToTxn(_ *sarama.ConsumerMessage, _ string, _ *string) error {
	return nil
}

// Mock: sarama.ConsumerGroupClaim

type mockClaim struct {
	messages chan *sarama.ConsumerMessage
}

func (m *mockClaim) Topic() string                            { return "test-topic" }
func (m *mockClaim) Partition() int32                         { return 0 }
func (m *mockClaim) InitialOffset() int64                     { return 0 }
func (m *mockClaim) HighWaterMarkOffset() int64               { return 0 }
func (m *mockClaim) Messages() <-chan *sarama.ConsumerMessage { return m.messages }

// Mock: sarama.ConsumerGroupSession

type mockSession struct {
	markedCount int
}

func (m *mockSession) Claims() map[string][]int32                       { return nil }
func (m *mockSession) MemberID() string                                 { return "" }
func (m *mockSession) GenerationID() int32                              { return 0 }
func (m *mockSession) MarkOffset(_ string, _ int32, _ int64, _ string)  {}
func (m *mockSession) Commit()                                          {}
func (m *mockSession) ResetOffset(_ string, _ int32, _ int64, _ string) {}
func (m *mockSession) MarkMessage(_ *sarama.ConsumerMessage, _ string)  { m.markedCount++ }
func (m *mockSession) Context() context.Context                         { return context.Background() }

// Mock: sarama.ConsumerGroup

type mockConsumerGroup struct {
	consumeErr error
	closed     bool
}

func (m *mockConsumerGroup) Consume(ctx context.Context, _ []string, _ sarama.ConsumerGroupHandler) error {
	<-ctx.Done() // block until context is cancelled, then return
	return m.consumeErr
}
func (m *mockConsumerGroup) Errors() <-chan error        { return nil }
func (m *mockConsumerGroup) Close() error                { m.closed = true; return nil }
func (m *mockConsumerGroup) Pause(_ map[string][]int32)  {}
func (m *mockConsumerGroup) Resume(_ map[string][]int32) {}
func (m *mockConsumerGroup) PauseAll()                   {}
func (m *mockConsumerGroup) ResumeAll()                  {}

// Producer tests

func TestKafkaProducerSendMessage(t *testing.T) {
	p := &kafkaProducer{producer: &mockSyncProducer{}}
	msg := types.NewMessageWithTopic("topic", "data")
	assert.NoError(t, p.SendMessage(context.Background(), msg))
}

func TestKafkaProducerSendMessageError(t *testing.T) {
	p := &kafkaProducer{producer: &mockSyncProducer{sendErr: errors.New("send failed")}}
	msg := types.NewMessageWithTopic("topic", "data")
	assert.Error(t, p.SendMessage(context.Background(), msg))
}

func TestKafkaProducerSendMessagesBatch(t *testing.T) {
	p := &kafkaProducer{producer: &mockSyncProducer{}}
	msgs := []*types.Message{
		types.NewMessageWithTopic("topic", "a"),
		types.NewMessageWithTopic("topic", "b"),
	}
	assert.NoError(t, p.SendMessagesBatch(context.Background(), msgs))
}

func TestKafkaProducerSendMessagesBatchError(t *testing.T) {
	p := &kafkaProducer{producer: &mockSyncProducer{batchErr: errors.New("batch failed")}}
	msgs := []*types.Message{types.NewMessageWithTopic("topic", "a")}
	assert.Error(t, p.SendMessagesBatch(context.Background(), msgs))
}

func TestKafkaProducerClose(t *testing.T) {
	p := &kafkaProducer{producer: &mockSyncProducer{}}
	assert.NoError(t, p.Close())
}

func TestKafkaProducerSendMessagePropagatesHeaders(t *testing.T) {
	mock := &mockSyncProducer{}
	p := &kafkaProducer{producer: mock}
	msg := types.NewMessageWithTopic("topic", "data").
		WithHeader("trace-id", "abc123").
		WithHeader("batch_id", "uuid-xyz")

	require.NoError(t, p.SendMessage(context.Background(), msg))
	require.NotNil(t, mock.lastMessage)
	require.Len(t, mock.lastMessage.Headers, 2)

	got := map[string]string{}
	for _, h := range mock.lastMessage.Headers {
		got[string(h.Key)] = string(h.Value)
	}
	assert.Equal(t, "abc123", got["trace-id"])
	assert.Equal(t, "uuid-xyz", got["batch_id"])
}

func TestKafkaProducerSendMessageNilHeadersSendsNilSlice(t *testing.T) {
	mock := &mockSyncProducer{}
	p := &kafkaProducer{producer: mock}
	msg := &types.Message{Topic: "topic", Value: []byte(`"v"`)}

	require.NoError(t, p.SendMessage(context.Background(), msg))
	require.NotNil(t, mock.lastMessage)
	assert.Nil(t, mock.lastMessage.Headers)
}

func TestKafkaProducerSendMessagesBatchPropagatesHeaders(t *testing.T) {
	mock := &mockSyncProducer{}
	p := &kafkaProducer{producer: mock}
	first := types.NewMessageWithTopic("topic", "a").WithHeader("batch_id", "B1")
	second := types.NewMessageWithTopic("topic", "b").WithHeader("batch_id", "B2")

	require.NoError(t, p.SendMessagesBatch(context.Background(), []*types.Message{first, second}))
	require.Len(t, mock.lastBatch, 2)
	require.Len(t, mock.lastBatch[0].Headers, 1)
	assert.Equal(t, "B1", string(mock.lastBatch[0].Headers[0].Value))
	assert.Equal(t, "B2", string(mock.lastBatch[1].Headers[0].Value))
}

func TestGroupHandlerConsumeClaimPropagatesHeaders(t *testing.T) {
	claim := &mockClaim{messages: make(chan *sarama.ConsumerMessage, 1)}
	claim.messages <- &sarama.ConsumerMessage{
		Topic:     "topic",
		Key:       []byte("k"),
		Value:     []byte(`"v"`),
		Timestamp: time.Now(),
		Headers: []*sarama.RecordHeader{
			{Key: []byte("trace-id"), Value: []byte("abc")},
			{Key: []byte("batch_id"), Value: []byte("xyz")},
		},
	}
	close(claim.messages)

	var captured *types.Message
	handler := port.MessageHandlerFunc(func(_ context.Context, m *types.Message) (types.Result, error) {
		captured = m
		return types.Ack, nil
	})
	h := &groupHandler{handler: handler, cfg: types.ConsumeConfig{Topic: "topic", AutoCommit: true}}

	require.NoError(t, h.ConsumeClaim(&mockSession{}, claim))
	require.NotNil(t, captured)
	assert.Equal(t, "abc", captured.Headers["trace-id"])
	assert.Equal(t, "xyz", captured.Headers["batch_id"])
}

func TestGroupHandlerConsumeClaimWithoutHeaders(t *testing.T) {
	claim := &mockClaim{messages: make(chan *sarama.ConsumerMessage, 1)}
	claim.messages <- &sarama.ConsumerMessage{
		Topic:     "topic",
		Value:     []byte(`"v"`),
		Timestamp: time.Now(),
	}
	close(claim.messages)

	var captured *types.Message
	handler := port.MessageHandlerFunc(func(_ context.Context, m *types.Message) (types.Result, error) {
		captured = m
		return types.Ack, nil
	})
	h := &groupHandler{handler: handler, cfg: types.ConsumeConfig{Topic: "topic", AutoCommit: true}}

	require.NoError(t, h.ConsumeClaim(&mockSession{}, claim))
	require.NotNil(t, captured)
	assert.Nil(t, captured.Headers)
}

func TestToRecordHeadersRoundTrip(t *testing.T) {
	in := map[string]string{"a": "1", "b": "2"}
	out := fromRecordHeaders(sliceToPtrs(toRecordHeaders(in)))
	assert.Equal(t, in, out)
}

func TestToRecordHeadersEmpty(t *testing.T) {
	assert.Nil(t, toRecordHeaders(nil))
	assert.Nil(t, toRecordHeaders(map[string]string{}))
}

func TestFromRecordHeadersEmpty(t *testing.T) {
	assert.Nil(t, fromRecordHeaders(nil))
	assert.Nil(t, fromRecordHeaders([]*sarama.RecordHeader{}))
}

// sliceToPtrs adapts the sarama.RecordHeader slice returned by toRecordHeaders
// (used by the producer) into the []*sarama.RecordHeader shape the consumer
// receives from Sarama — mirrors what Kafka does on the wire.
func sliceToPtrs(in []sarama.RecordHeader) []*sarama.RecordHeader {
	out := make([]*sarama.RecordHeader, 0, len(in))
	for i := range in {
		out = append(out, &in[i])
	}
	return out
}

// groupHandler tests

func TestGroupHandlerSetup(t *testing.T) {
	h := &groupHandler{}
	assert.NoError(t, h.Setup(nil))
}

func TestGroupHandlerCleanup(t *testing.T) {
	h := &groupHandler{}
	assert.NoError(t, h.Cleanup(nil))
}

func TestGroupHandlerConsumeClaimAck(t *testing.T) {
	ch := make(chan *sarama.ConsumerMessage, 1)
	ch <- &sarama.ConsumerMessage{Topic: "topic", Value: []byte(`"data"`)}
	close(ch)

	session := &mockSession{}
	claim := &mockClaim{messages: ch}
	h := &groupHandler{
		cfg: types.ConsumeConfig{AutoCommit: false},
		handler: port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
			return types.Ack, nil
		}),
	}
	require.NoError(t, h.ConsumeClaim(session, claim))
	assert.Equal(t, 1, session.markedCount) // MarkMessage called because !AutoCommit
}

func TestGroupHandlerConsumeClaimNack(t *testing.T) {
	ch := make(chan *sarama.ConsumerMessage, 1)
	ch <- &sarama.ConsumerMessage{Topic: "topic", Value: []byte(`"data"`)}
	close(ch)

	session := &mockSession{}
	claim := &mockClaim{messages: ch}
	h := &groupHandler{
		cfg: types.ConsumeConfig{MaxRetries: 0},
		handler: port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
			return types.Nack, errors.New("processing error")
		}),
	}
	require.NoError(t, h.ConsumeClaim(session, claim))
}

func TestGroupHandlerConsumeClaimIgnore(t *testing.T) {
	ch := make(chan *sarama.ConsumerMessage, 1)
	ch <- &sarama.ConsumerMessage{Topic: "topic", Value: []byte(`"data"`)}
	close(ch)

	session := &mockSession{}
	claim := &mockClaim{messages: ch}
	h := &groupHandler{
		cfg: types.ConsumeConfig{},
		handler: port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
			return types.Ignore, nil
		}),
	}
	require.NoError(t, h.ConsumeClaim(session, claim))
}

func TestGroupHandlerConsumeClaimWithDLQ(t *testing.T) {
	ch := make(chan *sarama.ConsumerMessage, 1)
	ch <- &sarama.ConsumerMessage{Topic: "topic", Value: []byte(`"data"`)}
	close(ch)

	claim := &mockClaim{messages: ch}
	session := &mockSession{}
	h := &groupHandler{
		cfg: types.ConsumeConfig{
			MaxRetries:      0,
			DeadLetterTopic: "dlq-topic",
		},
		handler: port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
			return types.Nack, errors.New("always fails")
		}),
	}
	// Should log DLQ error but not return error.
	require.NoError(t, h.ConsumeClaim(session, claim))
}

func TestGroupHandlerConsumeClaimWithRetry(t *testing.T) {
	ch := make(chan *sarama.ConsumerMessage, 1)
	ch <- &sarama.ConsumerMessage{Topic: "topic", Value: []byte(`"data"`)}
	close(ch)

	claim := &mockClaim{messages: ch}
	session := &mockSession{}

	attempts := 0
	h := &groupHandler{
		cfg: types.ConsumeConfig{
			MaxRetries:   1,
			RetryBackoff: time.Nanosecond, // fast retry
		},
		handler: port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
			attempts++
			if attempts < 2 {
				return types.Nack, errors.New("temporary")
			}
			return types.Ack, nil
		}),
	}
	require.NoError(t, h.ConsumeClaim(session, claim))
	assert.Equal(t, 2, attempts)
}

func TestGroupHandlerConsumeClaimAutoCommit(t *testing.T) {
	ch := make(chan *sarama.ConsumerMessage, 1)
	ch <- &sarama.ConsumerMessage{Topic: "topic", Value: []byte(`"data"`)}
	close(ch)

	session := &mockSession{}
	claim := &mockClaim{messages: ch}
	h := &groupHandler{
		cfg: types.ConsumeConfig{AutoCommit: true}, // MarkMessage must NOT be called
		handler: port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
			return types.Ack, nil
		}),
	}
	require.NoError(t, h.ConsumeClaim(session, claim))
	assert.Equal(t, 0, session.markedCount)
}

func TestGroupHandlerConsumeClaimDefaultRetryBackoff(t *testing.T) {
	// RetryBackoff <= 0 defaults to 1s — test that the branch is hit without
	// actually sleeping 1s by limiting MaxRetries to 0 and using Ignore result
	// so we only go through the retry loop once.
	ch := make(chan *sarama.ConsumerMessage, 1)
	ch <- &sarama.ConsumerMessage{Topic: "topic", Value: []byte(`"data"`)}
	close(ch)

	claim := &mockClaim{messages: ch}
	session := &mockSession{}
	h := &groupHandler{
		cfg: types.ConsumeConfig{MaxRetries: 0, RetryBackoff: 0},
		handler: port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
			return types.Ack, nil // break on first attempt
		}),
	}
	require.NoError(t, h.ConsumeClaim(session, claim))
}

// kafkaConsumer tests

func TestKafkaConsumerConsumeWithCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel so mock.Consume returns immediately

	mock := &mockConsumerGroup{}
	c := &kafkaConsumer{
		cfg:         types.ConsumeConfig{Topic: "topic"},
		concurrency: 1,
		newGroup:    func() (sarama.ConsumerGroup, error) { return mock, nil },
	}
	err := c.Consume(ctx, port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
		return types.Ack, nil
	}))
	assert.NoError(t, err)
	assert.True(t, mock.closed, "cada worker deve fechar o próprio ConsumerGroup via defer")
}

func TestKafkaConsumerConsumeGroupErrorIsLogged(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	mock := &mockConsumerGroup{consumeErr: errors.New("group error")}
	c := &kafkaConsumer{
		cfg:         types.ConsumeConfig{Topic: "topic"},
		concurrency: 1,
		newGroup:    func() (sarama.ConsumerGroup, error) { return mock, nil },
	}

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := c.Consume(ctx, port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
		return types.Ack, nil
	}))
	assert.NoError(t, err)
}

func TestKafkaConsumerClose(t *testing.T) {
	// Close é no-op: cada worker fecha o próprio ConsumerGroup via defer quando
	// Consume retorna (ctx cancelado). Validado em TestKafkaConsumerConsumeWithCancelledContext.
	c := &kafkaConsumer{}
	require.NoError(t, c.Close())
}

func TestKafkaConsumerConsumeGroupCreateErrorIsHandled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := &kafkaConsumer{
		cfg:         types.ConsumeConfig{Topic: "topic"},
		concurrency: 1,
		newGroup:    func() (sarama.ConsumerGroup, error) { return nil, errors.New("dial failed") },
	}
	err := c.Consume(ctx, port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
		return types.Ack, nil
	}))
	assert.NoError(t, err, "falha de criação de group num worker não derruba Consume")
}

func TestKafkaConsumerPauseAndResume(t *testing.T) {
	c := &kafkaConsumer{}
	assert.NoError(t, c.Pause())
	assert.NoError(t, c.Resume())
}

// Broker.Setup tests

// mockClusterAdmin implements the clusterAdmin interface.
type mockClusterAdmin struct {
	createErr    error
	createCalled []string
	closed       bool
}

func (m *mockClusterAdmin) CreateTopic(topic string, _ *sarama.TopicDetail, _ bool) error {
	m.createCalled = append(m.createCalled, topic)
	return m.createErr
}

func (m *mockClusterAdmin) Close() error {
	m.closed = true
	return nil
}

// newBrokerWithAdmin builds a Broker whose adminFactory returns the given mock.
func newBrokerWithAdmin(topics []TopicConfig, admin clusterAdmin) *Broker {
	return &Broker{
		brokers: []string{"localhost:9092"},
		config:  sarama.NewConfig(),
		topics:  topics,
		adminFactory: func(_ []string, _ *sarama.Config) (clusterAdmin, error) {
			return admin, nil
		},
	}
}

// Compile-time: Broker must satisfy port.BrokerSetup.
var _ interface{ Setup(context.Context) error } = (*Broker)(nil)

func TestBrokerSetupNoTopics(t *testing.T) {
	admin := &mockClusterAdmin{}
	b := newBrokerWithAdmin(nil, admin)
	assert.NoError(t, b.Setup(context.Background()))
	// adminFactory must NOT be called when there are no topics.
	assert.Empty(t, admin.createCalled)
	assert.False(t, admin.closed)
}

func TestBrokerSetupCreatesTopics(t *testing.T) {
	admin := &mockClusterAdmin{}
	topics := []TopicConfig{
		{Name: "orders", Partitions: 3, ReplicationFactor: 1},
		{Name: "payments", Partitions: 1, ReplicationFactor: 1},
	}
	b := newBrokerWithAdmin(topics, admin)
	require.NoError(t, b.Setup(context.Background()))
	assert.Equal(t, []string{"orders", "payments"}, admin.createCalled)
	assert.True(t, admin.closed)
}

func TestBrokerSetupIdempotentAlreadyExists(t *testing.T) {
	alreadyExists := &sarama.TopicError{Err: sarama.ErrTopicAlreadyExists}
	admin := &mockClusterAdmin{createErr: alreadyExists}
	b := newBrokerWithAdmin([]TopicConfig{{Name: "events"}}, admin)
	// Must NOT return an error when topic already exists.
	assert.NoError(t, b.Setup(context.Background()))
}

func TestBrokerSetupCreateTopicError(t *testing.T) {
	admin := &mockClusterAdmin{createErr: errors.New("broker unavailable")}
	b := newBrokerWithAdmin([]TopicConfig{{Name: "events"}}, admin)
	err := b.Setup(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "events")
}

func TestBrokerSetupAdminFactoryError(t *testing.T) {
	b := &Broker{
		brokers: []string{"localhost:9092"},
		config:  sarama.NewConfig(),
		topics:  []TopicConfig{{Name: "t"}},
		adminFactory: func(_ []string, _ *sarama.Config) (clusterAdmin, error) {
			return nil, errors.New("cannot connect")
		},
	}
	err := b.Setup(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create admin")
}

func TestBrokerSetupDefaultPartitionsAndReplication(t *testing.T) {
	var capturedDetail *sarama.TopicDetail
	admin := &mockClusterAdmin{}
	// Override CreateTopic to capture the detail.
	type capturingAdmin struct {
		*mockClusterAdmin
		detail *sarama.TopicDetail
	}

	capturing := &struct {
		mockClusterAdmin
		detail *sarama.TopicDetail
	}{}
	b := &Broker{
		brokers: []string{"localhost:9092"},
		config:  sarama.NewConfig(),
		topics:  []TopicConfig{{Name: "t", Partitions: 0, ReplicationFactor: 0}},
		adminFactory: func(_ []string, _ *sarama.Config) (clusterAdmin, error) {
			_ = capturing
			_ = admin
			return &capturingClusterAdmin{detail: &capturedDetail}, nil
		},
	}
	require.NoError(t, b.Setup(context.Background()))
	require.NotNil(t, capturedDetail)
	assert.Equal(t, int32(1), capturedDetail.NumPartitions)
	assert.Equal(t, int16(1), capturedDetail.ReplicationFactor)
}

type capturingClusterAdmin struct {
	detail **sarama.TopicDetail
	closed bool
}

func (c *capturingClusterAdmin) CreateTopic(_ string, detail *sarama.TopicDetail, _ bool) error {
	*c.detail = detail
	return nil
}

func (c *capturingClusterAdmin) Close() error {
	c.closed = true
	return nil
}
