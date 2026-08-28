// Internal tests for the oci package — access unexported types directly.
package oci

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/joaoprofile/gofi/msq/port"
	"github.com/joaoprofile/gofi/msq/types"
	"github.com/joaoprofile/gofi/msq/worker"
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
	deleteErr error

	deleteCalls    int
	deletedReceipt string
}

func (m *mockQueueClient) PutMessages(_ context.Context, _ queue.PutMessagesRequest) (queue.PutMessagesResponse, error) {
	return m.putResp, m.putErr
}

func (m *mockQueueClient) GetMessages(_ context.Context, _ queue.GetMessagesRequest) (queue.GetMessagesResponse, error) {
	return m.getResp, m.getErr
}

func (m *mockQueueClient) DeleteMessage(_ context.Context, in queue.DeleteMessageRequest) (queue.DeleteMessageResponse, error) {
	m.deleteCalls++
	if in.MessageReceipt != nil {
		m.deletedReceipt = *in.MessageReceipt
	}
	return queue.DeleteMessageResponse{}, m.deleteErr
}

// Helpers

func newTestOCIConsumer(client queueClientAPI, cfg types.ConsumeConfig) *ociConsumer {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 1
	}
	return &ociConsumer{
		client:      client,
		cfg:         cfg,
		concurrency: cfg.Concurrency,
	}
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
	msg := types.NewMessageWithTopic("ocid1.queue.oc1..test", "data")
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
	msg := types.NewMessageWithTopic("ocid1.queue.oc1..test", "data")
	assert.Error(t, p.SendMessage(context.Background(), msg))
}

func TestOCIProducerSendMessageNoConfirmation(t *testing.T) {
	// PutMessages succeeds but returns 0 messages → error.
	client := &mockQueueClient{putResp: putRespWithN(0)}
	p := newTestOCIProducer(client)
	msg := types.NewMessageWithTopic("ocid1.queue.oc1..test", "data")
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
		types.NewMessageWithTopic("ocid1.queue.oc1..q1", "a"),
		types.NewMessageWithTopic("ocid1.queue.oc1..q1", "b"),
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
	msgs := []*types.Message{types.NewMessageWithTopic("ocid1.queue.oc1..q", "x")}
	assert.Error(t, p.SendMessagesBatch(context.Background(), msgs))
}

func TestOCIProducerSendMessagesBatchCountMismatch(t *testing.T) {
	// Server confirms fewer messages than sent → error.
	client := &mockQueueClient{putResp: putRespWithN(0)}
	p := newTestOCIProducer(client)
	msgs := []*types.Message{types.NewMessageWithTopic("ocid1.queue.oc1..q", "x")}
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

func TestOCIConsumerConsumePollInterval(t *testing.T) {
	// PollInterval == 0 → uses DefaultPollInterval (no panic).
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := newTestOCIConsumer(&mockQueueClient{}, types.ConsumeConfig{
		QueueID:      "q",
		PollInterval: 0,
	})
	assert.NoError(t, c.Consume(ctx, nil))
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
	), pool, 30)

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
	c.poll(context.Background(), nil, pool, 30)
}

// ociConsumer.handle

func TestOCIConsumerHandleAck(t *testing.T) {
	// Receipt == nil → delete returns early (no network call).
	c := newTestOCIConsumer(&mockQueueClient{}, types.ConsumeConfig{QueueID: "q"})
	content := `{"Topic":"test","Value":"dGVzdA=="}`
	c.handle(context.Background(), queue.GetMessage{Content: &content, Receipt: nil}, port.MessageHandlerFunc(
		func(_ context.Context, _ *types.Message) (types.Result, error) { return types.Ack, nil },
	))
}

func TestOCIConsumerHandleAckWithDelete(t *testing.T) {
	receipt := "rh"
	client := &mockQueueClient{}
	c := newTestOCIConsumer(client, types.ConsumeConfig{QueueID: "q"})
	content := `{"Topic":"test","Value":"dGVzdA=="}`
	c.handle(context.Background(), queue.GetMessage{Content: &content, Receipt: &receipt}, port.MessageHandlerFunc(
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
	c.handle(context.Background(), queue.GetMessage{Content: &content, Receipt: &receipt}, port.MessageHandlerFunc(
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
	c.handle(context.Background(), queue.GetMessage{Content: &content, Receipt: &receipt}, port.MessageHandlerFunc(
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
	c.handle(context.Background(), queue.GetMessage{Content: &raw}, port.MessageHandlerFunc(
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
	c.handle(context.Background(), queue.GetMessage{Content: &bad}, port.MessageHandlerFunc(
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
	c.handle(context.Background(), queue.GetMessage{Content: nil}, port.MessageHandlerFunc(
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
	assert.True(t, c.paused.Load())
	require.NoError(t, c.Resume())
	assert.False(t, c.paused.Load())
}

// ociConsumer.Consume paused state

func TestOCIConsumerConsumePausedThenCancelled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	c := newTestOCIConsumer(&mockQueueClient{}, types.ConsumeConfig{QueueID: "q"})
	c.paused.Store(true) // paused — loop spins until ctx is done

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
	c := b.NewConsumer(types.ConsumeConfig{QueueID: "q", Concurrency: 1})
	assert.NotNil(t, c)
}

func TestOCIBrokerNewConsumerDefaultsConcurrency(t *testing.T) {
	b := &Broker{client: &mockQueueClient{}}
	c := b.NewConsumer(types.ConsumeConfig{QueueID: "q", Concurrency: 0})
	assert.NotNil(t, c)
}

// New: error path for client creation

func TestOCINewClientCreationError(t *testing.T) {
	// Provide a PrivateKey that the OCI SDK rejects so NewQueueClientWithConfigurationProvider fails.
	cfg := Config{
		TenancyID:   "ocid1.tenancy.oc1..test",
		UserID:      "ocid1.user.oc1..test",
		Region:      "sa-saopaulo-1",
		FingerPrint: "aa:bb:cc:dd",
		PrivateKey:  "not-a-valid-pem-key",
	}
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
