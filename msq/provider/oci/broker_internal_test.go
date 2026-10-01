// Internal tests for the oci package — access unexported types directly.
package oci

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cloudoci "github.com/gofi-labs/gofi-sdk-go/base/cloud/oci"
	"github.com/gofi-labs/gofi-sdk-go/msq/port"
	"github.com/gofi-labs/gofi-sdk-go/msq/types"
	"github.com/gofi-labs/gofi-sdk-go/msq/worker"
	ocicommon "github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/queue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock: queueClientAPI

type mockQueueClient struct {
	putErr    error
	putResp   queue.PutMessagesResponse
	getErr    error
	getResp   queue.GetMessagesResponse
	getOnce   bool // serve getResp once (up to the limit), then empty
	deleteErr error

	mu             sync.Mutex
	deleteCalls    int
	deletedReceipt string
	limits         []int // Limit of every GetMessages
	visibilities   []int // VisibilityInSeconds of every GetMessages
	renewed        []int // VisibilityInSeconds of every UpdateMessage
}

func (m *mockQueueClient) PutMessages(_ context.Context, _ queue.PutMessagesRequest) (queue.PutMessagesResponse, error) {
	return m.putResp, m.putErr
}

func (m *mockQueueClient) GetMessages(ctx context.Context, in queue.GetMessagesRequest) (queue.GetMessagesResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.limits = append(m.limits, *in.Limit)
	m.visibilities = append(m.visibilities, *in.VisibilityInSeconds)
	if !m.getOnce {
		return m.getResp, m.getErr
	}
	n := min(*in.Limit, len(m.getResp.Messages))
	resp := queue.GetMessagesResponse{GetMessages: queue.GetMessages{Messages: m.getResp.Messages[:n]}}
	m.getResp.Messages = m.getResp.Messages[n:]
	if n == 0 {
		m.mu.Unlock()
		select { // emulate long polling
		case <-ctx.Done():
		case <-time.After(5 * time.Millisecond):
		}
		m.mu.Lock()
	}
	return resp, m.getErr
}

func (m *mockQueueClient) DeleteMessage(_ context.Context, in queue.DeleteMessageRequest) (queue.DeleteMessageResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteCalls++
	if in.MessageReceipt != nil {
		m.deletedReceipt = *in.MessageReceipt
	}
	return queue.DeleteMessageResponse{}, m.deleteErr
}

func (m *mockQueueClient) UpdateMessage(_ context.Context, in queue.UpdateMessageRequest) (queue.UpdateMessageResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.renewed = append(m.renewed, *in.VisibilityInSeconds)
	return queue.UpdateMessageResponse{}, nil
}

// Helpers

func newTestOCIConsumer(client queueClientAPI, cfg types.ConsumeConfig) *ociConsumer {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 1
	}
	return newConsumer(client, cfg, cfg.Concurrency)
}

func newTestOCIProducer(client queueClientAPI) *ociProducer {
	return &ociProducer{client: client}
}

// putRespWithN returns a PutMessagesResponse with n confirmed messages.
func putRespWithN(n int) queue.PutMessagesResponse {
	msgs := make([]queue.PutMessage, n)
	for i := range msgs {
		id := int64(i)
		msgs[i] = queue.PutMessage{Id: &id}
	}
	return queue.PutMessagesResponse{
		PutMessages: queue.PutMessages{Messages: msgs},
	}
}

// ociProducer.SendMessage

func TestOCIProducerSendMessageSuccess(t *testing.T) {
	client := &mockQueueClient{putResp: putRespWithN(1)}
	p := newTestOCIProducer(client)
	msg := testMessageWithTopic("ocid1.queue.oc1..test", "data")
	assert.NoError(t, p.SendMessage(context.Background(), msg))
}

func TestOCIProducerSendMessageEmptyTopic(t *testing.T) {
	p := newTestOCIProducer(&mockQueueClient{})
	msg := &types.Message{Topic: ""}
	err := p.SendMessage(context.Background(), msg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "queue OCID")
}

func TestOCIProducerSendMessagePutError(t *testing.T) {
	client := &mockQueueClient{putErr: errors.New("put failed")}
	p := newTestOCIProducer(client)
	msg := testMessageWithTopic("ocid1.queue.oc1..test", "data")
	assert.Error(t, p.SendMessage(context.Background(), msg))
}

func TestOCIProducerSendMessageNoConfirmation(t *testing.T) {
	// PutMessages succeeds but returns 0 messages → error.
	client := &mockQueueClient{putResp: putRespWithN(0)}
	p := newTestOCIProducer(client)
	msg := testMessageWithTopic("ocid1.queue.oc1..test", "data")
	err := p.SendMessage(context.Background(), msg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no message confirmation")
}

func TestOCIProducerClose(t *testing.T) {
	p := newTestOCIProducer(&mockQueueClient{})
	assert.NoError(t, p.Close())
}

// ociProducer.SendMessagesBatch

func TestOCIProducerSendMessagesBatchSuccess(t *testing.T) {
	client := &mockQueueClient{putResp: putRespWithN(2)}
	p := newTestOCIProducer(client)
	msgs := []*types.Message{
		testMessageWithTopic("ocid1.queue.oc1..q1", "a"),
		testMessageWithTopic("ocid1.queue.oc1..q1", "b"),
	}
	assert.NoError(t, p.SendMessagesBatch(context.Background(), msgs))
}

func TestOCIProducerSendMessagesBatchEmpty(t *testing.T) {
	p := newTestOCIProducer(&mockQueueClient{})
	assert.NoError(t, p.SendMessagesBatch(context.Background(), nil))
}

func TestOCIProducerSendMessagesBatchPutError(t *testing.T) {
	client := &mockQueueClient{putErr: errors.New("put failed")}
	p := newTestOCIProducer(client)
	msgs := []*types.Message{testMessageWithTopic("ocid1.queue.oc1..q", "x")}
	assert.Error(t, p.SendMessagesBatch(context.Background(), msgs))
}

func TestOCIProducerSendMessagesBatchCountMismatch(t *testing.T) {
	// Server confirms fewer messages than sent → error.
	client := &mockQueueClient{putResp: putRespWithN(0)}
	p := newTestOCIProducer(client)
	msgs := []*types.Message{testMessageWithTopic("ocid1.queue.oc1..q", "x")}
	err := p.SendMessagesBatch(context.Background(), msgs)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected")
}

// ociConsumer.Consume

func TestOCIConsumerConsumeEmptyQueueID(t *testing.T) {
	c := newTestOCIConsumer(&mockQueueClient{}, types.ConsumeConfig{})
	err := c.Consume(context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "QueueID")
}

func TestOCIConsumerConsumeCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := newTestOCIConsumer(&mockQueueClient{}, types.ConsumeConfig{QueueID: "q"})
	err := c.Consume(ctx, nil)
	assert.NoError(t, err)
}

// PollInterval used to be the visibility (default 10s): messages were
// redelivered to another consumer while their handler still ran.
func TestOCIConsumerVisibilityIsNotPollInterval(t *testing.T) {
	assert.Equal(t, 30, newTestOCIConsumer(&mockQueueClient{}, types.ConsumeConfig{QueueID: "q", PollInterval: time.Second}).visibility)
	assert.Equal(t, 90, newTestOCIConsumer(&mockQueueClient{}, types.ConsumeConfig{QueueID: "q", VisibilityTimeout: 90 * time.Second}).visibility)
	assert.Equal(t, 1, newTestOCIConsumer(&mockQueueClient{}, types.ConsumeConfig{QueueID: "q", VisibilityTimeout: time.Millisecond}).visibility)
}

func ociMessages(n int) []queue.GetMessage {
	msgs := make([]queue.GetMessage, n)
	for i := range msgs {
		content, receipt := `{"value":"v"}`, "r-"+strconv.Itoa(i)
		msgs[i] = queue.GetMessage{Content: &content, Receipt: &receipt}
	}
	return msgs
}

// Receiving more than the free workers let messages burn their visibility in
// the pool buffer; a slow handler must keep renewing its own.
func TestOCIConsumerReceivesOnlyWhatCanStartAndRenews(t *testing.T) {
	client := &mockQueueClient{getOnce: true, getResp: queue.GetMessagesResponse{GetMessages: queue.GetMessages{Messages: ociMessages(5)}}}
	c := newTestOCIConsumer(client, types.ConsumeConfig{QueueID: "q", Concurrency: 2, VisibilityTimeout: 300 * time.Millisecond})
	release := make(chan struct{})
	var running, handled atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Consume(ctx, port.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) {
			running.Add(1)
			<-release
			handled.Add(1)
			return types.Ack, nil
		}))
	}()
	require.Eventually(t, func() bool { return running.Load() == 2 }, 2*time.Second, 5*time.Millisecond)
	time.Sleep(250 * time.Millisecond) // > one renewal period (100ms)
	client.mu.Lock()
	gets := len(client.limits)
	assert.GreaterOrEqual(t, len(client.renewed), 2, "busy handlers renew their lease")
	client.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	client.mu.Lock()
	assert.Equal(t, gets, len(client.limits), "no receive while every worker is busy")
	client.mu.Unlock()

	close(release)
	require.Eventually(t, func() bool { return handled.Load() == 5 }, 2*time.Second, 5*time.Millisecond)
	cancel()
	<-done

	client.mu.Lock()
	defer client.mu.Unlock()
	for i, n := range client.limits {
		assert.LessOrEqual(t, n, 2)
		assert.Equal(t, 1, client.visibilities[i])
	}
	for _, v := range client.renewed {
		assert.Equal(t, 1, v)
	}
	assert.Equal(t, 5, client.deleteCalls)
	renewals := len(client.renewed)
	client.mu.Unlock()
	time.Sleep(150 * time.Millisecond)
	client.mu.Lock()
	assert.Len(t, client.renewed, renewals, "no renewal after the delete")
}

// ociConsumer.poll

func TestOCIConsumerPollDeliversMessages(t *testing.T) {
	content := `{"Topic":"t","Value":"dGVzdA=="}`
	receipt := "receipt-1"
	client := &mockQueueClient{
		getResp: queue.GetMessagesResponse{
			GetMessages: queue.GetMessages{
				Messages: []queue.GetMessage{{
					Content: &content,
					Receipt: &receipt,
				}},
			},
		},
	}

	handled := make(chan struct{}, 1)
	c := newTestOCIConsumer(client, types.ConsumeConfig{QueueID: "q"})
	pool := worker.New(1)

	c.poll(context.Background(), port.MessageHandlerFunc(
		func(_ context.Context, _ *types.Message) (types.Result, error) {
			handled <- struct{}{}
			return types.Ack, nil
		},
	), pool, worker.NewSlots(1), 1)

	pool.Close()
	select {
	case <-handled:
		// message handled ✓
	default:
		t.Fatal("handler was never called")
	}
}

func TestOCIConsumerPollGetError(t *testing.T) {
	client := &mockQueueClient{getErr: errors.New("get failed")}
	c := newTestOCIConsumer(client, types.ConsumeConfig{QueueID: "q"})
	pool := worker.New(1)
	defer pool.Close()
	// Must not panic; error is only logged.
	c.poll(context.Background(), nil, pool, worker.NewSlots(1), 1)
}

// ociConsumer.handle

func TestOCIConsumerHandleAck(t *testing.T) {
	// Receipt == nil → delete returns early (no network call).
	c := newTestOCIConsumer(&mockQueueClient{}, types.ConsumeConfig{QueueID: "q"})
	content := `{"Topic":"test","Value":"dGVzdA=="}`
	c.handleNoLease(context.Background(), queue.GetMessage{Content: &content, Receipt: nil}, port.MessageHandlerFunc(
		func(_ context.Context, _ *types.Message) (types.Result, error) { return types.Ack, nil },
	))
}

func TestOCIConsumerHandleAckWithDelete(t *testing.T) {
	receipt := "rh"
	client := &mockQueueClient{}
	c := newTestOCIConsumer(client, types.ConsumeConfig{QueueID: "q"})
	content := `{"Topic":"test","Value":"dGVzdA=="}`
	c.handleNoLease(context.Background(), queue.GetMessage{Content: &content, Receipt: &receipt}, port.MessageHandlerFunc(
		func(_ context.Context, _ *types.Message) (types.Result, error) { return types.Ack, nil },
	))
	assert.Equal(t, 1, client.deleteCalls)
	assert.Equal(t, receipt, client.deletedReceipt)
}

func TestOCIConsumerHandleAckDeleteError(t *testing.T) {
	receipt := "rh"
	client := &mockQueueClient{deleteErr: errors.New("delete failed")}
	c := newTestOCIConsumer(client, types.ConsumeConfig{QueueID: "q"})
	content := `{"Topic":"test","Value":"dGVzdA=="}`
	// Must not propagate the delete error.
	c.handleNoLease(context.Background(), queue.GetMessage{Content: &content, Receipt: &receipt}, port.MessageHandlerFunc(
		func(_ context.Context, _ *types.Message) (types.Result, error) { return types.Ack, nil },
	))
}

// handleResult runs handle with a fixed result and returns the mock client.
func handleResult(t *testing.T, result types.Result) *mockQueueClient {
	t.Helper()
	client := &mockQueueClient{}
	c := newTestOCIConsumer(client, types.ConsumeConfig{QueueID: "q"})
	content := `{"Topic":"test","Value":"dGVzdA=="}`
	receipt := "rh"
	c.handleNoLease(context.Background(), queue.GetMessage{Content: &content, Receipt: &receipt}, port.MessageHandlerFunc(
		func(_ context.Context, _ *types.Message) (types.Result, error) { return result, nil },
	))
	return client
}

func TestOCIConsumerHandleNack(t *testing.T) {
	client := handleResult(t, types.Nack)
	assert.Equal(t, 0, client.deleteCalls) // kept for the visibility timeout to requeue
}

// Ignore must delete: on a visibility-timeout queue, not deleting is a requeue.
func TestOCIConsumerHandleIgnore(t *testing.T) {
	client := handleResult(t, types.Ignore)
	assert.Equal(t, 1, client.deleteCalls)
	assert.Equal(t, "rh", client.deletedReceipt)
}

// An unknown result must not delete.
func TestOCIConsumerHandleUnknownResult(t *testing.T) {
	client := handleResult(t, types.Result(99))
	assert.Equal(t, 0, client.deleteCalls)
}

// Valid JSON that is not a types.Message envelope must reach the handler as a
// raw body, not as an empty Value.
func TestOCIConsumerHandleRawNotificationBody(t *testing.T) {
	raw := `{"NotificationType":"AnyOfferChanged","Payload":{"SellerId":"A1B2C3"}}`
	called := false
	c := newTestOCIConsumer(&mockQueueClient{}, types.ConsumeConfig{QueueID: "q"})
	c.handleNoLease(context.Background(), queue.GetMessage{Content: &raw}, port.MessageHandlerFunc(
		func(_ context.Context, msg *types.Message) (types.Result, error) {
			called = true
			assert.Equal(t, raw, string(msg.Value))
			return types.Ack, nil
		},
	))
	assert.True(t, called)
}

func TestOCIConsumerHandleInvalidJSON(t *testing.T) {
	c := newTestOCIConsumer(&mockQueueClient{}, types.ConsumeConfig{QueueID: "q"})
	bad := `{broken`
	called := false
	c.handleNoLease(context.Background(), queue.GetMessage{Content: &bad}, port.MessageHandlerFunc(
		func(_ context.Context, msg *types.Message) (types.Result, error) {
			called = true
			assert.Equal(t, []byte(bad), []byte(msg.Value))
			return types.Nack, nil
		},
	))
	assert.True(t, called)
}

func TestOCIConsumerHandleNilContent(t *testing.T) {
	c := newTestOCIConsumer(&mockQueueClient{}, types.ConsumeConfig{QueueID: "q"})
	called := false
	c.handleNoLease(context.Background(), queue.GetMessage{Content: nil}, port.MessageHandlerFunc(
		func(_ context.Context, _ *types.Message) (types.Result, error) {
			called = true
			return types.Nack, nil
		},
	))
	assert.True(t, called)
}

// ociConsumer.delete

func TestOCIConsumerDeleteNilReceipt(t *testing.T) {
	c := newTestOCIConsumer(&mockQueueClient{}, types.ConsumeConfig{QueueID: "q"})
	// Must return immediately without calling the client.
	c.delete(context.Background(), queue.GetMessage{Receipt: nil})
}

func TestOCIConsumerDeleteSuccess(t *testing.T) {
	receipt := "rh"
	c := newTestOCIConsumer(&mockQueueClient{}, types.ConsumeConfig{QueueID: "q"})
	c.delete(context.Background(), queue.GetMessage{Receipt: &receipt})
}

func TestOCIConsumerDeleteError(t *testing.T) {
	receipt := "rh"
	c := newTestOCIConsumer(&mockQueueClient{deleteErr: errors.New("del failed")}, types.ConsumeConfig{QueueID: "q"})
	// Error only logged, must not panic.
	c.delete(context.Background(), queue.GetMessage{Receipt: &receipt})
}

// ociConsumer Close / Pause / Resume

func TestOCIConsumerCloseIsNoop(t *testing.T) {
	c := newTestOCIConsumer(&mockQueueClient{}, types.ConsumeConfig{QueueID: "q"})
	assert.NoError(t, c.Close())
}

func TestOCIConsumerPauseAndResume(t *testing.T) {
	c := newTestOCIConsumer(&mockQueueClient{}, types.ConsumeConfig{QueueID: "q"})
	require.NoError(t, c.Pause())
	assert.True(t, c.gate.Paused())
	require.NoError(t, c.Resume())
	assert.False(t, c.gate.Paused())
}

// ociConsumer.Consume paused state

func TestOCIConsumerConsumePausedThenCancelled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	c := newTestOCIConsumer(&mockQueueClient{}, types.ConsumeConfig{QueueID: "q"})
	c.gate.Pause()

	err := c.Consume(ctx, nil)
	assert.NoError(t, err)
}

// Broker.NewProducer / NewConsumer (use real *Broker via New)

func TestOCIBrokerNewProducer(t *testing.T) {
	b := &Broker{client: &mockQueueClient{}}
	p, err := b.NewProducer()
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestOCIBrokerNewConsumer(t *testing.T) {
	b := &Broker{client: &mockQueueClient{}}
	c, _ := b.NewConsumer(types.ConsumeConfig{QueueID: "q", Concurrency: 1})
	assert.NotNil(t, c)
}

func TestOCIBrokerNewConsumerDefaultsConcurrency(t *testing.T) {
	b := &Broker{client: &mockQueueClient{}}
	c, _ := b.NewConsumer(types.ConsumeConfig{QueueID: "q", Concurrency: 0})
	assert.NotNil(t, c)
}

// New: error path for client creation

func TestOCINewClientCreationError(t *testing.T) {
	// Provide a PrivateKey that the OCI SDK rejects so NewQueueClientWithConfigurationProvider fails.
	cfg := Config{Credentials: cloudoci.Config{
		TenancyID:   "ocid1.tenancy.oc1..test",
		UserID:      "ocid1.user.oc1..test",
		Region:      "sa-saopaulo-1",
		Fingerprint: "aa:bb:cc:dd",
		PrivateKey:  "not-a-valid-pem-key",
	}}
	// The OCI SDK may or may not fail at construction depending on validation
	// timing; we only assert no panic.
	_, _ = New(cfg)
}

// Verify *queue.QueueClient satisfies queueClientAPI at compile time

var _ queueClientAPI = (*ociQueueClientWrapper)(nil)

// ociQueueClientWrapper delegates to a real *queue.QueueClient.
// This compile-time check ensures the interface matches the SDK.
type ociQueueClientWrapper struct{ c *queue.QueueClient }

func (w *ociQueueClientWrapper) PutMessages(ctx context.Context, req queue.PutMessagesRequest) (queue.PutMessagesResponse, error) {
	return w.c.PutMessages(ctx, req)
}
func (w *ociQueueClientWrapper) GetMessages(ctx context.Context, req queue.GetMessagesRequest) (queue.GetMessagesResponse, error) {
	return w.c.GetMessages(ctx, req)
}
func (w *ociQueueClientWrapper) DeleteMessage(ctx context.Context, req queue.DeleteMessageRequest) (queue.DeleteMessageResponse, error) {
	return w.c.DeleteMessage(ctx, req)
}
func (w *ociQueueClientWrapper) UpdateMessage(ctx context.Context, req queue.UpdateMessageRequest) (queue.UpdateMessageResponse, error) {
	return w.c.UpdateMessage(ctx, req)
}

// Verify that New() produces a Broker whose client field satisfies queueClientAPI.
func TestOCINewAssignsQueueClientAsInterface(t *testing.T) {
	provider := ocicommon.NewRawConfigurationProvider(
		"ocid1.tenancy.oc1..test", "ocid1.user.oc1..test",
		"sa-saopaulo-1", "aa:bb:cc:dd:ee:ff:00:11:22:33:44:55:66:77:88:99",
		"", nil,
	)
	client, err := queue.NewQueueClientWithConfigurationProvider(provider)
	if err != nil {
		t.Skip("OCI SDK rejected provider — skip interface check")
	}
	b := &Broker{client: &client}
	assert.NotNil(t, b.client)
}

func TestNew_DefaultEndpointFollowsRegion(t *testing.T) {
	b, err := New(Config{Credentials: cloudoci.Config{
		TenancyID: "t", UserID: "u", Region: "us-ashburn-1", Fingerprint: "f", PrivateKey: pemKey(t),
	}})
	if err != nil {
		t.Fatal(err)
	}
	host := b.client.(*queue.QueueClient).Host
	if host != "https://cell-1.queue.messaging.us-ashburn-1.oci.oraclecloud.com" {
		t.Fatalf("endpoint=%s", host)
	}
}

func pemKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

// handleNoLease runs handle for a message without a lease renewal.
func (c *ociConsumer) handleNoLease(ctx context.Context, m queue.GetMessage, h port.MessageHandler) {
	c.handle(ctx, m, h, func() {})
}

// Topic is the queue consumed, DeliveryCount the queue's counter, and a
// foreign body keeps an Id derived from the OCI message id.
func TestOCIConsumerHandleTransportMetadata(t *testing.T) {
	c := newTestOCIConsumer(&mockQueueClient{}, types.ConsumeConfig{QueueID: "ocid1.queue.x"})
	spoofed := `{"id":"6f1d2a8c-9e10-4b1e-9f6e-5b0a3f3e3c55","topic":"evil","value":"v"}`
	var got *types.Message
	c.handleNoLease(context.Background(), queue.GetMessage{Content: &spoofed, DeliveryCount: new(3)}, port.MessageHandlerFunc(
		func(_ context.Context, m *types.Message) (types.Result, error) { got = m; return types.Ack, nil }))
	assert.Equal(t, "ocid1.queue.x", got.Topic)
	assert.Equal(t, 3, got.DeliveryCount)

	raw := "plain"
	ids := map[string]bool{}
	for range 2 {
		c.handleNoLease(context.Background(), queue.GetMessage{Content: &raw, Id: new(int64(42))}, port.MessageHandlerFunc(
			func(_ context.Context, m *types.Message) (types.Result, error) {
				ids[m.Id.String()] = true
				return types.Nack, nil
			}))
	}
	assert.Len(t, ids, 1, "a redelivered foreign message keeps its Id")
}

func TestOCIConsumerHandleReject(t *testing.T) {
	client := handleResult(t, types.Reject)
	assert.Equal(t, 1, client.deleteCalls)
}
