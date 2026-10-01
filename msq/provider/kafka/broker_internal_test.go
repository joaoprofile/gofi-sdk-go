// Internal tests for the kafka package — access unexported types directly.
package kafka

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/gofi-labs/gofi-sdk-go/msq/port"
	"github.com/gofi-labs/gofi-sdk-go/msq/types"
	"github.com/google/uuid"
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
	ctx         context.Context
}

func (m *mockSession) Claims() map[string][]int32                       { return nil }
func (m *mockSession) MemberID() string                                 { return "" }
func (m *mockSession) GenerationID() int32                              { return 0 }
func (m *mockSession) MarkOffset(_ string, _ int32, _ int64, _ string)  {}
func (m *mockSession) Commit()                                          {}
func (m *mockSession) ResetOffset(_ string, _ int32, _ int64, _ string) {}
func (m *mockSession) MarkMessage(_ *sarama.ConsumerMessage, _ string)  { m.markedCount++ }
func (m *mockSession) Context() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

// Mock: sarama.ConsumerGroup

type mockConsumerGroup struct {
	consumeErr error
	closed     bool
	pausedAll  atomic.Bool
}

func (m *mockConsumerGroup) Consume(ctx context.Context, _ []string, _ sarama.ConsumerGroupHandler) error {
	<-ctx.Done() // block until context is cancelled, then return
	return m.consumeErr
}
func (m *mockConsumerGroup) Errors() <-chan error        { return nil }
func (m *mockConsumerGroup) Close() error                { m.closed = true; return nil }
func (m *mockConsumerGroup) Pause(_ map[string][]int32)  {}
func (m *mockConsumerGroup) Resume(_ map[string][]int32) {}
func (m *mockConsumerGroup) PauseAll()                   { m.pausedAll.Store(true) }
func (m *mockConsumerGroup) ResumeAll()                  { m.pausedAll.Store(false) }

// Producer tests

func TestKafkaProducerSendMessage(t *testing.T) {
	p := &kafkaProducer{producer: &mockSyncProducer{}}
	msg := testMessageWithTopic("topic", "data")
	assert.NoError(t, p.SendMessage(context.Background(), msg))
}

func TestKafkaProducerSendMessageError(t *testing.T) {
	p := &kafkaProducer{producer: &mockSyncProducer{sendErr: errors.New("send failed")}}
	msg := testMessageWithTopic("topic", "data")
	assert.Error(t, p.SendMessage(context.Background(), msg))
}

func TestKafkaProducerSendMessagesBatch(t *testing.T) {
	p := &kafkaProducer{producer: &mockSyncProducer{}}
	msgs := []*types.Message{
		testMessageWithTopic("topic", "a"),
		testMessageWithTopic("topic", "b"),
	}
	assert.NoError(t, p.SendMessagesBatch(context.Background(), msgs))
}

func TestKafkaProducerSendMessagesBatchError(t *testing.T) {
	p := &kafkaProducer{producer: &mockSyncProducer{batchErr: errors.New("batch failed")}}
	msgs := []*types.Message{testMessageWithTopic("topic", "a")}
	assert.Error(t, p.SendMessagesBatch(context.Background(), msgs))
}

func TestKafkaProducerClose(t *testing.T) {
	p := &kafkaProducer{producer: &mockSyncProducer{}}
	assert.NoError(t, p.Close())
}

func headerMap(hs []sarama.RecordHeader) map[string]string {
	out := map[string]string{}
	for _, h := range hs {
		out[string(h.Key)] = string(h.Value)
	}
	return out
}

func TestKafkaProducerSendMessagePropagatesHeaders(t *testing.T) {
	mock := &mockSyncProducer{}
	p := &kafkaProducer{producer: mock}
	msg := testMessageWithTopic("topic", "data").
		WithHeader("trace-id", "abc123").
		WithHeader("batch_id", "uuid-xyz")

	require.NoError(t, p.SendMessage(context.Background(), msg))
	require.NotNil(t, mock.lastMessage)
	got := headerMap(mock.lastMessage.Headers)
	assert.Equal(t, "abc123", got["trace-id"])
	assert.Equal(t, "uuid-xyz", got["batch_id"])
}

func TestKafkaProducerWritesCloudEventsBinary(t *testing.T) {
	mock := &mockSyncProducer{}
	p := &kafkaProducer{producer: mock}
	msg := testMessageWithTopic("topic", "data")
	msg.Type = "order.created"

	require.NoError(t, p.SendMessage(context.Background(), msg))
	got := headerMap(mock.lastMessage.Headers)
	assert.Equal(t, "1.0", got["ce_specversion"])
	assert.Equal(t, msg.Id.String(), got["ce_id"])
	assert.Equal(t, "order.created", got["ce_type"])
	assert.Equal(t, "application/json", got["content-type"])
	assert.Nil(t, mock.lastMessage.Key, "empty key keeps round-robin partitioning")
}

func TestKafkaRoundTripPreservesMessage(t *testing.T) {
	mock := &mockSyncProducer{}
	p := &kafkaProducer{producer: mock}
	in := testMessageWithTopic("topic", "data").WithKey("k").WithHeader("tenant", "a")
	require.NoError(t, p.SendMessage(context.Background(), in))

	pm := mock.lastMessage
	value, _ := pm.Value.Encode()
	key, _ := pm.Key.Encode()
	out := decode(&sarama.ConsumerMessage{Topic: pm.Topic, Key: key, Value: value, Headers: sliceToPtrs(pm.Headers)})

	assert.Equal(t, in.Id, out.Id)
	assert.Equal(t, "k", out.Key)
	assert.JSONEq(t, string(in.Value), string(out.Value))
	assert.True(t, in.Timestamp.Equal(out.Timestamp))
	assert.Equal(t, map[string]string{"tenant": "a"}, out.Headers)
}

func TestKafkaDecodeRecordWithoutCloudEvents(t *testing.T) {
	ts := time.Now()
	out := decode(&sarama.ConsumerMessage{Topic: "topic", Value: []byte(`"v"`), Timestamp: ts,
		Headers: []*sarama.RecordHeader{{Key: []byte("x"), Value: []byte("1")}}})
	assert.NotEqual(t, uuid.Nil, out.Id)
	assert.Equal(t, ts, out.Timestamp)
	assert.Equal(t, "1", out.Headers["x"])
}

func TestKafkaProducerSendMessagesBatchPropagatesHeaders(t *testing.T) {
	mock := &mockSyncProducer{}
	p := &kafkaProducer{producer: mock}
	first := testMessageWithTopic("topic", "a").WithHeader("batch_id", "B1")
	second := testMessageWithTopic("topic", "b").WithHeader("batch_id", "B2")

	require.NoError(t, p.SendMessagesBatch(context.Background(), []*types.Message{first, second}))
	require.Len(t, mock.lastBatch, 2)
	assert.Equal(t, "B1", headerMap(mock.lastBatch[0].Headers)["batch_id"])
	assert.Equal(t, "B2", headerMap(mock.lastBatch[1].Headers)["batch_id"])
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
	h := &groupHandler{handler: handler, cfg: types.ConsumeConfig{Topic: "topic"}}

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
	h := &groupHandler{handler: handler, cfg: types.ConsumeConfig{Topic: "topic"}}

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
		cfg: types.ConsumeConfig{},
		handler: port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
			return types.Ack, nil
		}),
	}
	require.NoError(t, h.ConsumeClaim(session, claim))
	assert.Equal(t, 1, session.markedCount)
}

// A Nack must never be committed: the record is redelivered in place until it
// settles, and the records behind it wait (per-partition order).
func TestGroupHandlerConsumeClaimNackIsRedeliveredInPlace(t *testing.T) {
	ch := make(chan *sarama.ConsumerMessage, 2)
	ch <- &sarama.ConsumerMessage{Topic: "topic", Offset: 1, Value: []byte(`"first"`)}
	ch <- &sarama.ConsumerMessage{Topic: "topic", Offset: 2, Value: []byte(`"second"`)}
	close(ch)

	var seen []string
	session := &mockSession{}
	h := &groupHandler{
		cfg: types.ConsumeConfig{RetryBackoff: time.Millisecond},
		handler: port.MessageHandlerFunc(func(_ context.Context, m *types.Message) (types.Result, error) {
			seen = append(seen, string(m.Value))
			if len(seen) < 3 {
				return types.Nack, errors.New("processing error")
			}
			return types.Ack, nil
		}),
	}
	require.NoError(t, h.ConsumeClaim(session, &mockClaim{messages: ch}))
	assert.Equal(t, []string{`"first"`, `"first"`, `"first"`, `"second"`}, seen)
	assert.Equal(t, 2, session.markedCount)
}

// A record that keeps failing (e.g. the dead-letter publish fails) stays
// unmarked when the partition is revoked; nothing behind it is processed.
func TestGroupHandlerConsumeClaimPersistentNackIsNeverMarked(t *testing.T) {
	ch := make(chan *sarama.ConsumerMessage, 2)
	ch <- &sarama.ConsumerMessage{Topic: "topic", Offset: 1, Value: []byte(`"poison"`)}
	ch <- &sarama.ConsumerMessage{Topic: "topic", Offset: 2, Value: []byte(`"next"`)}
	close(ch)

	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	session := &mockSession{ctx: ctx}
	h := &groupHandler{
		cfg: types.ConsumeConfig{RetryBackoff: time.Millisecond},
		handler: port.MessageHandlerFunc(func(_ context.Context, m *types.Message) (types.Result, error) {
			if string(m.Value) != `"poison"` {
				t.Error("a record behind an unsettled one was processed")
			}
			if calls.Add(1) == 5 {
				cancel() // rebalance
			}
			return types.Nack, errors.New("dead-letter publish failed")
		}),
	}
	require.NoError(t, h.ConsumeClaim(session, &mockClaim{messages: ch}))
	assert.Equal(t, int32(5), calls.Load())
	assert.Equal(t, 0, session.markedCount)
}

// A Nack while the partition is being revoked leaves the record unmarked so
// the next owner reprocesses it.
func TestGroupHandlerConsumeClaimNackOnRevokeIsNotMarked(t *testing.T) {
	ch := make(chan *sarama.ConsumerMessage, 1)
	ch <- &sarama.ConsumerMessage{Topic: "topic", Value: []byte(`"data"`)}
	close(ch)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	session := &mockSession{ctx: ctx}
	h := &groupHandler{
		handler: port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
			return types.Nack, context.Canceled
		}),
	}
	require.NoError(t, h.ConsumeClaim(session, &mockClaim{messages: ch}))
	assert.Equal(t, 0, session.markedCount)
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

// kafkaConsumer tests

func TestKafkaConsumerConsumeWithCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel so mock.Consume returns immediately

	mock := &mockConsumerGroup{}
	c := &kafkaConsumer{
		cfg:      types.ConsumeConfig{Topic: "topic"},
		newGroup: func() (sarama.ConsumerGroup, error) { return mock, nil },
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
		cfg:      types.ConsumeConfig{Topic: "topic"},
		newGroup: func() (sarama.ConsumerGroup, error) { return mock, nil },
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
	// Close is a no-op: each worker closes its own ConsumerGroup via defer when
	// Consume returns (ctx cancelled). Covered by TestKafkaConsumerConsumeWithCancelledContext.
	c := &kafkaConsumer{}
	require.NoError(t, c.Close())
}

func TestKafkaConsumerConsumeGroupCreateErrorIsReturned(t *testing.T) {
	c := &kafkaConsumer{
		cfg:      types.ConsumeConfig{Topic: "topic"},
		newGroup: func() (sarama.ConsumerGroup, error) { return nil, errors.New("dial failed") },
	}
	err := c.Consume(context.Background(), port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
		return types.Ack, nil
	}))
	assert.ErrorContains(t, err, "dial failed")
}

func TestKafkaConsumerPauseAndResume(t *testing.T) {
	mock := &mockConsumerGroup{}
	c := &kafkaConsumer{cfg: types.ConsumeConfig{Topic: "topic"}, newGroup: func() (sarama.ConsumerGroup, error) { return mock, nil }}
	require.NoError(t, c.Pause(), "pause before Consume is remembered")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Consume(ctx, port.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) { return types.Ack, nil }))
	}()
	require.Eventually(t, func() bool { return mock.pausedAll.Load() }, time.Second, time.Millisecond)

	require.NoError(t, c.Resume())
	assert.False(t, mock.pausedAll.Load())
	cancel()
	<-done
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
	b := &Broker{
		brokers: []string{"localhost:9092"},
		config:  sarama.NewConfig(),
		topics:  []TopicConfig{{Name: "t", Partitions: 0, ReplicationFactor: 0}},
		adminFactory: func(_ []string, _ *sarama.Config) (clusterAdmin, error) {
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

// Each in-place redelivery is a delivery: the pipeline sees the count grow and
// its Reject (delivery limit reached) settles the record so the partition moves on.
func TestGroupHandlerConsumeClaimCountsDeliveriesAndSettlesReject(t *testing.T) {
	ch := make(chan *sarama.ConsumerMessage, 2)
	ch <- &sarama.ConsumerMessage{Topic: "topic", Offset: 1, Value: []byte(`"poison"`)}
	ch <- &sarama.ConsumerMessage{Topic: "topic", Offset: 2, Value: []byte(`"next"`)}
	close(ch)

	var counts []int
	var ids []string
	session := &mockSession{}
	h := &groupHandler{
		cfg: types.ConsumeConfig{RetryBackoff: time.Millisecond, MaxDeliveries: 3},
		handler: port.MessageHandlerFunc(func(_ context.Context, m *types.Message) (types.Result, error) {
			if string(m.Value) == `"next"` {
				return types.Ack, nil
			}
			counts = append(counts, m.DeliveryCount)
			ids = append(ids, m.Id.String())
			if m.DeliveryCount >= 3 {
				return types.Reject, errors.New("poison")
			}
			return types.Nack, errors.New("poison")
		}),
	}
	require.NoError(t, h.ConsumeClaim(session, &mockClaim{messages: ch}))
	assert.Equal(t, []int{1, 2, 3}, counts)
	assert.Equal(t, ids[0], ids[2], "the Id is stable across redeliveries")
	assert.Equal(t, 2, session.markedCount, "a rejected record is committed")
}
