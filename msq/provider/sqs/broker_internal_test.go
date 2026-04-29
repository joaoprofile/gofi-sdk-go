package sqs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	awssqs "github.com/aws/aws-sdk-go/service/sqs"
	"github.com/aws/aws-sdk-go/service/sqs/sqsiface"
	"github.com/joaoprofile/gofi/msq/port"
	"github.com/joaoprofile/gofi/msq/types"
	"github.com/joaoprofile/gofi/msq/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock: sqsiface.SQSAPI

// mockSQS implements sqsiface.SQSAPI with configurable returns.
// Unused methods are provided via the embedded interface (zero-value; will panic if called).
type mockSQS struct {
	sqsiface.SQSAPI

	queueURL   string
	getURLErr  error
	sendErr    error
	batchOut   *awssqs.SendMessageBatchOutput
	batchErr   error
	receiveOut *awssqs.ReceiveMessageOutput
	receiveErr error
	deleteErr  error
}

func (m *mockSQS) GetQueueUrlWithContext(_ context.Context, in *awssqs.GetQueueUrlInput, _ ...request.Option) (*awssqs.GetQueueUrlOutput, error) {
	if m.getURLErr != nil {
		return nil, m.getURLErr
	}
	url := m.queueURL
	if url == "" {
		url = "https://sqs.us-east-1.amazonaws.com/123/" + aws.StringValue(in.QueueName)
	}
	return &awssqs.GetQueueUrlOutput{QueueUrl: &url}, nil
}

func (m *mockSQS) SendMessageWithContext(_ context.Context, _ *awssqs.SendMessageInput, _ ...request.Option) (*awssqs.SendMessageOutput, error) {
	id := "msg-id"
	return &awssqs.SendMessageOutput{MessageId: &id}, m.sendErr
}

func (m *mockSQS) SendMessageBatchWithContext(_ context.Context, _ *awssqs.SendMessageBatchInput, _ ...request.Option) (*awssqs.SendMessageBatchOutput, error) {
	if m.batchErr != nil {
		return nil, m.batchErr
	}
	if m.batchOut != nil {
		return m.batchOut, nil
	}
	return &awssqs.SendMessageBatchOutput{}, nil
}

func (m *mockSQS) ReceiveMessageWithContext(_ context.Context, _ *awssqs.ReceiveMessageInput, _ ...request.Option) (*awssqs.ReceiveMessageOutput, error) {
	if m.receiveErr != nil {
		return nil, m.receiveErr
	}
	if m.receiveOut != nil {
		return m.receiveOut, nil
	}
	return &awssqs.ReceiveMessageOutput{}, nil
}

func (m *mockSQS) DeleteMessage(_ *awssqs.DeleteMessageInput) (*awssqs.DeleteMessageOutput, error) {
	return &awssqs.DeleteMessageOutput{}, m.deleteErr
}

// Helpers

func newTestProducer(client sqsiface.SQSAPI) *sqsProducer {
	return &sqsProducer{client: client}
}

func newTestConsumer(client sqsiface.SQSAPI) *sqsConsumer {
	return &sqsConsumer{
		client:      client,
		cfg:         types.ConsumeConfig{Topic: "test-queue"},
		concurrency: 1,
	}
}

func sqsBody(msg *types.Message) *string {
	b, _ := json.Marshal(msg)
	s := string(b)
	return &s
}

// sqsProducer.SendMessage

func TestSQSProducerSendMessageSuccess(t *testing.T) {
	p := newTestProducer(&mockSQS{})
	msg := types.NewMessageWithTopic("test-queue", "data")
	assert.NoError(t, p.SendMessage(context.Background(), msg))
}

func TestSQSProducerSendMessageResolveError(t *testing.T) {
	p := newTestProducer(&mockSQS{getURLErr: errors.New("not found")})
	msg := types.NewMessageWithTopic("q", "data")
	assert.Error(t, p.SendMessage(context.Background(), msg))
}

func TestSQSProducerSendMessageSendError(t *testing.T) {
	p := newTestProducer(&mockSQS{sendErr: errors.New("send failed")})
	msg := types.NewMessageWithTopic("q", "data")
	assert.Error(t, p.SendMessage(context.Background(), msg))
}

// sqsProducer.SendMessagesBatch / sendChunk

func TestSQSProducerSendMessagesBatchSuccess(t *testing.T) {
	p := newTestProducer(&mockSQS{})
	msgs := []*types.Message{
		types.NewMessageWithTopic("q", "a"),
		types.NewMessageWithTopic("q", "b"),
	}
	assert.NoError(t, p.SendMessagesBatch(context.Background(), msgs))
}

func TestSQSProducerSendMessagesBatchResolveError(t *testing.T) {
	p := newTestProducer(&mockSQS{getURLErr: errors.New("not found")})
	msgs := []*types.Message{types.NewMessageWithTopic("q", "x")}
	assert.Error(t, p.SendMessagesBatch(context.Background(), msgs))
}

func TestSQSProducerSendChunkBatchError(t *testing.T) {
	p := newTestProducer(&mockSQS{batchErr: errors.New("batch failed")})
	url := aws.String("https://fake/q")
	msgs := []*types.Message{types.NewMessageWithTopic("q", "x")}
	err := p.sendChunk(context.Background(), url, msgs)
	assert.Error(t, err)
}

func TestSQSProducerSendChunkPartialFailure(t *testing.T) {
	failID := "0"
	p := newTestProducer(&mockSQS{
		batchOut: &awssqs.SendMessageBatchOutput{
			Failed: []*awssqs.BatchResultErrorEntry{{Id: &failID}},
		},
	})
	url := aws.String("https://fake/q")
	msgs := []*types.Message{types.NewMessageWithTopic("q", "x")}
	err := p.sendChunk(context.Background(), url, msgs)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "entries failed")
}

// sqsConsumer.Consume

func TestSQSConsumerConsumeContextCancelAfterResolve(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately — loop exits via ctx.Done()

	c := newTestConsumer(&mockSQS{})
	err := c.Consume(ctx, port.MessageHandlerFunc(
		func(_ context.Context, _ *types.Message) (types.Result, error) {
			return types.Ack, nil
		},
	))
	assert.NoError(t, err)
}

func TestSQSConsumerConsumeResolveError(t *testing.T) {
	c := newTestConsumer(&mockSQS{getURLErr: errors.New("not found")})
	err := c.Consume(context.Background(), nil)
	assert.Error(t, err)
}

// sqsConsumer.poll

func TestSQSConsumerPollDeliversMessages(t *testing.T) {
	receipt := "rh"
	mock := &mockSQS{
		receiveOut: &awssqs.ReceiveMessageOutput{
			Messages: []*awssqs.Message{{
				MessageId:     aws.String("id"),
				ReceiptHandle: &receipt,
				Body:          sqsBody(types.NewMessageWithTopic("q", "hello")),
			}},
		},
	}

	handled := make(chan struct{}, 1)
	c := newTestConsumer(mock)
	url := aws.String("https://fake/q")

	pool := worker.New(c.concurrency)

	c.poll(context.Background(), url, port.MessageHandlerFunc(
		func(_ context.Context, _ *types.Message) (types.Result, error) {
			handled <- struct{}{}
			return types.Ack, nil
		},
	), pool)

	// Close pool to drain all enqueued work, then verify handler was called.
	pool.Close()
	select {
	case <-handled:
		// message was handled ✓
	default:
		t.Fatal("handler was never called")
	}
}

func TestSQSConsumerPollReceiveError(t *testing.T) {
	c := newTestConsumer(&mockSQS{receiveErr: errors.New("receive failed")})
	url := aws.String("https://fake/q")
	pool := worker.New(1)
	defer pool.Close()
	// Must not panic; error is only logged.
	c.poll(context.Background(), url, nil, pool)
}

// sqsConsumer.handle

func TestSQSConsumerHandleAck(t *testing.T) {
	c := newTestConsumer(&mockSQS{})
	msg := types.NewMessageWithTopic("q", "payload")
	c.handle(context.Background(), aws.String("https://fake/q"), &awssqs.Message{Body: sqsBody(msg), ReceiptHandle: aws.String("rh")},
		port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) { return types.Ack, nil }))
}

func TestSQSConsumerHandleNack(t *testing.T) {
	c := newTestConsumer(&mockSQS{})
	msg := types.NewMessageWithTopic("q", "payload")
	c.handle(context.Background(), aws.String("https://fake/q"), &awssqs.Message{Body: sqsBody(msg), ReceiptHandle: aws.String("rh")},
		port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) { return types.Nack, nil }))
}

func TestSQSConsumerHandleIgnore(t *testing.T) {
	c := newTestConsumer(&mockSQS{})
	msg := types.NewMessageWithTopic("q", "payload")
	c.handle(context.Background(), aws.String("https://fake/q"), &awssqs.Message{Body: sqsBody(msg), ReceiptHandle: aws.String("rh")},
		port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) { return types.Ignore, nil }))
}

func TestSQSConsumerHandleInvalidJSON(t *testing.T) {
	c := newTestConsumer(&mockSQS{})
	raw := `{not json}`
	called := false
	c.handle(context.Background(), aws.String("https://fake/q"), &awssqs.Message{Body: &raw, ReceiptHandle: aws.String("rh")},
		port.MessageHandlerFunc(func(_ context.Context, msg *types.Message) (types.Result, error) {
			called = true
			assert.Equal(t, raw, string(msg.Value))
			return types.Nack, nil
		}))
	assert.True(t, called)
}

// Body is valid JSON but not a types.Message envelope (e.g., a raw SNS-to-SQS
// notification from an external producer). json.Unmarshal succeeds with all
// zero values, so the handler must receive the raw body as Value.
func TestSQSConsumerHandleRawNotificationBody(t *testing.T) {
	c := newTestConsumer(&mockSQS{})
	raw := `{"NotificationType":"AnyOfferChanged","Payload":{"SellerId":"A1B2C3"}}`
	var got *types.Message
	c.handle(context.Background(), aws.String("https://fake/q"), &awssqs.Message{Body: &raw, ReceiptHandle: aws.String("rh")},
		port.MessageHandlerFunc(func(_ context.Context, msg *types.Message) (types.Result, error) {
			got = msg
			return types.Ack, nil
		}))
	require.NotNil(t, got)
	assert.Equal(t, raw, string(got.Value), "raw body must be exposed as Value when envelope is absent")

	// Downstream must be able to unmarshal Value into the notification model.
	var decoded struct {
		NotificationType string
		Payload          struct{ SellerId string }
	}
	require.NoError(t, json.Unmarshal(got.Value, &decoded))
	assert.Equal(t, "AnyOfferChanged", decoded.NotificationType)
	assert.Equal(t, "A1B2C3", decoded.Payload.SellerId)
}

// Envelope-wrapped messages (produced via types.NewMessage) must keep
// behaving as before: handler receives the inner Value, not the whole envelope.
func TestSQSConsumerHandleEnvelopePreservesValue(t *testing.T) {
	c := newTestConsumer(&mockSQS{})
	payload := map[string]string{"hello": "world"}
	envelope := types.NewMessageWithTopic("q", payload)

	var got *types.Message
	c.handle(context.Background(), aws.String("https://fake/q"), &awssqs.Message{Body: sqsBody(envelope), ReceiptHandle: aws.String("rh")},
		port.MessageHandlerFunc(func(_ context.Context, msg *types.Message) (types.Result, error) {
			got = msg
			return types.Ack, nil
		}))
	require.NotNil(t, got)

	var decoded map[string]string
	require.NoError(t, json.Unmarshal(got.Value, &decoded))
	assert.Equal(t, payload, decoded)
}

func TestSQSConsumerHandleAckDeleteError(t *testing.T) {
	// delete fails → error only logged; handle must not propagate it.
	c := newTestConsumer(&mockSQS{deleteErr: errors.New("delete failed")})
	msg := types.NewMessageWithTopic("q", "payload")
	c.handle(context.Background(), aws.String("https://fake/q"), &awssqs.Message{Body: sqsBody(msg), ReceiptHandle: aws.String("rh")},
		port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) { return types.Ack, nil }))
}

func TestSQSConsumerHandleHandlerError(t *testing.T) {
	c := newTestConsumer(&mockSQS{})
	msg := types.NewMessageWithTopic("q", "payload")
	c.handle(context.Background(), aws.String("https://fake/q"), &awssqs.Message{Body: sqsBody(msg), ReceiptHandle: aws.String("rh")},
		port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
			return types.Nack, errors.New("handler failed")
		}))
}

// sqsConsumer.resolveURL

func TestSQSConsumerResolveURLNilQueueURL(t *testing.T) {
	// GetQueueUrl returns a nil QueueUrl — should return error.
	nilURLMock := &mockSQS{}
	nilURLMock.queueURL = "" // will set a non-nil URL; adjust mock to test nil path
	// The nil-URL branch requires a custom mock that returns nil QueueUrl.
	// Use the existing error path test (TestConsumerConsumeReturnsErrorWhenQueueNotFound
	// in broker_test.go) for the nil-not-found case.
	// Here we just verify the happy path returns a URL.
	c := newTestConsumer(nilURLMock)
	url, err := c.resolveURL(context.Background(), "test-queue")
	require.NoError(t, err)
	assert.NotNil(t, url)
}

// sqsConsumer Pause / Resume

func TestSQSConsumerPauseAndResume(t *testing.T) {
	c := newTestConsumer(&mockSQS{})
	require.NoError(t, c.Pause())
	assert.True(t, c.paused.Load())
	require.NoError(t, c.Resume())
	assert.False(t, c.paused.Load())
}

// sqsProducer.Close

func TestSQSProducerClose(t *testing.T) {
	p := newTestProducer(&mockSQS{})
	assert.NoError(t, p.Close())
}

// sqsConsumer.resolveURL — nil QueueUrl path

// nilURLMock returns a non-nil GetQueueUrlOutput but with a nil QueueUrl field.
type nilURLMock struct{ sqsiface.SQSAPI }

func (m *nilURLMock) GetQueueUrlWithContext(_ context.Context, _ *awssqs.GetQueueUrlInput, _ ...request.Option) (*awssqs.GetQueueUrlOutput, error) {
	return &awssqs.GetQueueUrlOutput{QueueUrl: nil}, nil
}

func TestSQSConsumerResolveURLReturnsErrorWhenQueueUrlIsNil(t *testing.T) {
	c := newTestConsumer(&nilURLMock{})
	url, err := c.resolveURL(context.Background(), "missing-queue")
	assert.Nil(t, url)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// sqsConsumer.Consume — paused loop path

func TestSQSConsumerConsumePausedThenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := newTestConsumer(&mockSQS{})
	c.paused.Store(true)

	done := make(chan error, 1)
	go func() {
		done <- c.Consume(ctx, port.MessageHandlerFunc(
			func(_ context.Context, _ *types.Message) (types.Result, error) {
				return types.Ack, nil
			},
		))
	}()

	// Give the goroutine time to enter the paused spin loop, then cancel.
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Consume did not return after context cancellation")
	}
}
