package sqs

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	cloudaws "github.com/gofi-labs/gofi-sdk-go/base/cloud/aws"
	"github.com/gofi-labs/gofi-sdk-go/msq/port"
	"github.com/gofi-labs/gofi-sdk-go/msq/types"
	"github.com/gofi-labs/gofi-sdk-go/obs/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	logging.NewLogger("sqs-test")
	m.Run()
}

type fakeSQS struct {
	mu          sync.Mutex
	urlCalls    atomic.Int32
	receives    atomic.Int32
	receiveErr  error
	messages    []sqstypes.Message // returned once
	sent        []string
	lastSend    *awssqs.SendMessageInput
	lastReceive *awssqs.ReceiveMessageInput
	batches     [][]sqstypes.SendMessageBatchRequestEntry
	batchFailed []sqstypes.BatchResultErrorEntry
	deleted     []string
	deleteCtx   error
	maxAsked    []int32  // MaxNumberOfMessages of every receive
	renewed     []string // receipts whose visibility was changed
	renewedVis  int32
}

func (f *fakeSQS) GetQueueUrl(_ context.Context, in *awssqs.GetQueueUrlInput, _ ...func(*awssqs.Options)) (*awssqs.GetQueueUrlOutput, error) {
	f.urlCalls.Add(1)
	if *in.QueueName == "missing" {
		return nil, errors.New("AWS.SimpleQueueService.NonExistentQueue")
	}
	return &awssqs.GetQueueUrlOutput{QueueUrl: awssdk.String("https://sqs/" + *in.QueueName)}, nil
}

func (f *fakeSQS) SendMessage(_ context.Context, in *awssqs.SendMessageInput, _ ...func(*awssqs.Options)) (*awssqs.SendMessageOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, *in.MessageBody)
	f.lastSend = in
	return &awssqs.SendMessageOutput{}, nil
}

func (f *fakeSQS) SendMessageBatch(_ context.Context, in *awssqs.SendMessageBatchInput, _ ...func(*awssqs.Options)) (*awssqs.SendMessageBatchOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batches = append(f.batches, in.Entries)
	return &awssqs.SendMessageBatchOutput{Failed: f.batchFailed}, nil
}

func (f *fakeSQS) ReceiveMessage(ctx context.Context, in *awssqs.ReceiveMessageInput, _ ...func(*awssqs.Options)) (*awssqs.ReceiveMessageOutput, error) {
	f.receives.Add(1)
	f.mu.Lock()
	f.lastReceive = in
	f.mu.Unlock()
	if f.receiveErr != nil {
		return nil, f.receiveErr
	}
	f.mu.Lock()
	f.maxAsked = append(f.maxAsked, in.MaxNumberOfMessages)
	n := min(int(in.MaxNumberOfMessages), len(f.messages))
	msgs := f.messages[:n]
	f.messages = f.messages[n:]
	f.mu.Unlock()
	if len(msgs) == 0 {
		select { // emulate long polling
		case <-ctx.Done():
		case <-time.After(10 * time.Millisecond):
		}
	}
	return &awssqs.ReceiveMessageOutput{Messages: msgs}, nil
}

func (f *fakeSQS) DeleteMessage(ctx context.Context, in *awssqs.DeleteMessageInput, _ ...func(*awssqs.Options)) (*awssqs.DeleteMessageOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, *in.ReceiptHandle)
	f.deleteCtx = ctx.Err()
	return &awssqs.DeleteMessageOutput{}, nil
}

func (f *fakeSQS) ChangeMessageVisibility(_ context.Context, in *awssqs.ChangeMessageVisibilityInput, _ ...func(*awssqs.Options)) (*awssqs.ChangeMessageVisibilityOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.renewed = append(f.renewed, *in.ReceiptHandle)
	f.renewedVis = in.VisibilityTimeout
	return &awssqs.ChangeMessageVisibilityOutput{}, nil
}

func sqsMessage(t *testing.T, receipt string, value any) sqstypes.Message {
	b, err := json.Marshal(testMessageWithTopic("q", value))
	require.NoError(t, err)
	return sqstypes.Message{ReceiptHandle: awssdk.String(receipt), Body: awssdk.String(string(b))}
}

func TestProducer_CachesQueueURL(t *testing.T) {
	f := &fakeSQS{}
	p, _ := newBroker(f).NewProducer()
	for range 3 {
		require.NoError(t, p.SendMessage(context.Background(), testMessageWithTopic("orders", "v")))
	}
	assert.Equal(t, int32(1), f.urlCalls.Load(), "GetQueueUrl must be called once per queue")
	assert.Len(t, f.sent, 3)
}

func TestProducer_BatchChunksAndReportsFailures(t *testing.T) {
	f := &fakeSQS{}
	p, _ := newBroker(f).NewProducer()
	msgs := make([]*types.Message, 23)
	for i := range msgs {
		msgs[i] = testMessageWithTopic("orders", i)
	}
	require.NoError(t, p.SendMessagesBatch(context.Background(), msgs))
	require.Len(t, f.batches, 3)
	assert.Len(t, f.batches[0], 10)
	assert.Len(t, f.batches[2], 3)

	f.batchFailed = []sqstypes.BatchResultErrorEntry{{Id: awssdk.String("4"), Message: awssdk.String("throttled")}}
	err := p.SendMessagesBatch(context.Background(), msgs[:2])
	assert.ErrorContains(t, err, "4: throttled")
}

func TestProducer_UnknownQueue(t *testing.T) {
	p, _ := newBroker(&fakeSQS{}).NewProducer()
	assert.ErrorContains(t, p.SendMessage(context.Background(), testMessageWithTopic("missing", "v")), "missing")
}

func consumeUntil(t *testing.T, f *fakeSQS, h port.MessageHandler, stop func() bool) {
	t.Helper()
	c, _ := newBroker(f).NewConsumer(types.ConsumeConfig{Topic: "q", Concurrency: 1})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = c.Consume(ctx, h); close(done) }()
	require.Eventually(t, stop, 2*time.Second, 5*time.Millisecond)
	cancel()
	<-done
}

func TestConsumer_AckAndIgnoreDeleteNackDoesNot(t *testing.T) {
	f := &fakeSQS{}
	f.messages = []sqstypes.Message{sqsMessage(t, "r-ack", "a"), sqsMessage(t, "r-nack", "n"), sqsMessage(t, "r-ignore", "i")}
	var handled atomic.Int32
	h := port.MessageHandlerFunc(func(_ context.Context, m *types.Message) (types.Result, error) {
		defer handled.Add(1)
		switch string(m.Value) {
		case `"n"`:
			return types.Nack, nil
		case `"i"`:
			return types.Ignore, nil
		}
		return types.Ack, nil
	})
	consumeUntil(t, f, h, func() bool { return handled.Load() == 3 })

	f.mu.Lock()
	defer f.mu.Unlock()
	assert.ElementsMatch(t, []string{"r-ack", "r-ignore"}, f.deleted)
	assert.NoError(t, f.deleteCtx, "delete must not use a canceled context")
}

func TestConsumer_BacksOffOnReceiveErrors(t *testing.T) {
	f := &fakeSQS{receiveErr: errors.New("AccessDenied")}
	c, _ := newBroker(f).NewConsumer(types.ConsumeConfig{Topic: "q"})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = c.Consume(ctx, port.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) { return types.Ack, nil }))
	assert.LessOrEqual(t, f.receives.Load(), int32(10))
}

func TestConsumer_PausedDoesNotPoll(t *testing.T) {
	f := &fakeSQS{}
	c, _ := newBroker(f).NewConsumer(types.ConsumeConfig{Topic: "q"})
	require.NoError(t, c.Pause())
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = c.Consume(ctx, port.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) { return types.Ack, nil }))
	assert.Zero(t, f.receives.Load())
}

func TestConsumer_RawBodyIsDelivered(t *testing.T) {
	f := &fakeSQS{messages: []sqstypes.Message{{ReceiptHandle: awssdk.String("r"), Body: awssdk.String("plain text")}}}
	var got atomic.Value
	consumeUntil(t, f, port.MessageHandlerFunc(func(_ context.Context, m *types.Message) (types.Result, error) {
		got.Store(string(m.Value))
		return types.Ack, nil
	}), func() bool { return got.Load() != nil })
	assert.Equal(t, "plain text", got.Load())
}

func TestNew_UsesAWSConfig(t *testing.T) {
	t.Setenv("AWS_CONFIG_FILE", "/nonexistent")
	b, err := New(context.Background(), Config{AWS: cloudaws.Config{Region: "us-east-1"}})
	require.NoError(t, err)
	assert.NotNil(t, b)
}

func TestNew_RequiresRegion(t *testing.T) {
	t.Setenv("AWS_CONFIG_FILE", "/nonexistent")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	_, err := New(context.Background(), Config{})
	require.ErrorContains(t, err, "region is required")
}

func TestProducer_FIFOSetsGroupAndDedup(t *testing.T) {
	f := &fakeSQS{}
	p, _ := newBroker(f).NewProducer()
	m := testMessageWithTopic("orders.fifo", "v").WithKey("customer-1")
	require.NoError(t, p.SendMessage(context.Background(), m))
	assert.Equal(t, "customer-1", awssdk.ToString(f.lastSend.MessageGroupId))
	assert.Equal(t, m.Id.String(), awssdk.ToString(f.lastSend.MessageDeduplicationId))

	require.NoError(t, p.SendMessagesBatch(context.Background(), []*types.Message{testMessageWithTopic("orders.fifo", "v")}))
	assert.Equal(t, defaultGroupID, awssdk.ToString(f.batches[0][0].MessageGroupId))
	assert.NotEmpty(t, awssdk.ToString(f.batches[0][0].MessageDeduplicationId))
}

func TestProducer_StandardQueueHasNoGroup(t *testing.T) {
	f := &fakeSQS{}
	p, _ := newBroker(f).NewProducer()
	require.NoError(t, p.SendMessage(context.Background(), testMessageWithTopic("orders", "v").WithKey("k")))
	assert.Nil(t, f.lastSend.MessageGroupId)
}

// Messages of one FIFO group are handled in order even with many workers.
func TestConsumer_FIFOKeepsGroupOrder(t *testing.T) {
	f := &fakeSQS{}
	for i := range 10 {
		m := sqsMessage(t, strconv.Itoa(i), i)
		m.Attributes = map[string]string{"MessageGroupId": "g" + strconv.Itoa(i%2)}
		f.messages = append(f.messages, m)
	}
	c, _ := newBroker(f).NewConsumer(types.ConsumeConfig{Topic: "orders.fifo", Concurrency: 10})

	var mu sync.Mutex
	seen := map[string][]int{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Consume(ctx, port.MessageHandlerFunc(func(_ context.Context, m *types.Message) (types.Result, error) {
			v, _ := types.UnpackMessage[int](m)
			time.Sleep(time.Duration(10-*v) * time.Millisecond) // later messages finish faster
			mu.Lock()
			seen["g"+strconv.Itoa(*v%2)] = append(seen["g"+strconv.Itoa(*v%2)], *v)
			mu.Unlock()
			return types.Ack, nil
		}))
	}()
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(seen["g0"])+len(seen["g1"]) == 10 }, 2*time.Second, 5*time.Millisecond)
	cancel()
	<-done

	assert.Equal(t, []int{0, 2, 4, 6, 8}, seen["g0"])
	assert.Equal(t, []int{1, 3, 5, 7, 9}, seen["g1"])
	f.mu.Lock()
	defer f.mu.Unlock()
	assert.Contains(t, f.lastReceive.MessageSystemAttributeNames, sqstypes.MessageSystemAttributeNameMessageGroupId)
}

// Receiving more than the free workers let messages burn their visibility in
// the pool buffer before a handler even started.
func TestConsumer_ReceivesOnlyWhatCanStart(t *testing.T) {
	f := &fakeSQS{}
	for i := range 5 {
		f.messages = append(f.messages, sqsMessage(t, strconv.Itoa(i), i))
	}
	c, _ := newBroker(f).NewConsumer(types.ConsumeConfig{Topic: "q", Concurrency: 2})
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
	receives := f.receives.Load()
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, receives, f.receives.Load(), "no receive while every worker is busy")
	close(release)
	require.Eventually(t, func() bool { return handled.Load() == 5 }, 2*time.Second, 5*time.Millisecond)
	cancel()
	<-done

	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.maxAsked {
		assert.LessOrEqual(t, n, int32(2))
		assert.Positive(t, n)
	}
	assert.Equal(t, int32(30), f.lastReceive.VisibilityTimeout, "DefaultVisibilityTimeout")
}

// A handler slower than the visibility timeout keeps its message invisible:
// the lease is renewed until the message is deleted, never after.
func TestConsumer_RenewsVisibilityWhileHandling(t *testing.T) {
	f := &fakeSQS{messages: []sqstypes.Message{sqsMessage(t, "r-slow", "v")}}
	c, _ := newBroker(f).NewConsumer(types.ConsumeConfig{Topic: "q", Concurrency: 1, VisibilityTimeout: 300 * time.Millisecond})
	var handled atomic.Bool
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Consume(ctx, port.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) {
			time.Sleep(350 * time.Millisecond)
			handled.Store(true)
			return types.Ack, nil
		}))
	}()
	require.Eventually(t, handled.Load, 2*time.Second, 5*time.Millisecond)
	cancel()
	<-done

	f.mu.Lock()
	renewals := len(f.renewed)
	assert.GreaterOrEqual(t, renewals, 2)
	assert.Equal(t, "r-slow", f.renewed[0])
	assert.Equal(t, int32(1), f.renewedVis, "rounded to at least one second")
	assert.Equal(t, int32(1), f.lastReceive.VisibilityTimeout)
	assert.Equal(t, []string{"r-slow"}, f.deleted)
	f.mu.Unlock()
	time.Sleep(250 * time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	assert.Len(t, f.renewed, renewals, "no renewal after the delete")
}

// Topic is the queue consumed, never the payload's; DeliveryCount comes from
// ApproximateReceiveCount and a foreign body keeps the SQS MessageId as Id.
func TestConsumer_TransportMetadata(t *testing.T) {
	spoofed := sqsMessage(t, "r1", "v")
	spoofed.Body = awssdk.String(strings.Replace(*spoofed.Body, `"topic":"q"`, `"topic":"evil"`, 1))
	spoofed.Attributes = map[string]string{"ApproximateReceiveCount": "4"}
	raw := sqstypes.Message{ReceiptHandle: awssdk.String("r2"), Body: awssdk.String("plain"), MessageId: awssdk.String("1b9f6c1e-8c1a-4a9e-9d6e-6f1d2a8c9e10")}
	f := &fakeSQS{messages: []sqstypes.Message{spoofed, raw}}
	var mu sync.Mutex
	got := map[string]*types.Message{}
	consumeUntil(t, f, port.MessageHandlerFunc(func(_ context.Context, m *types.Message) (types.Result, error) {
		mu.Lock()
		defer mu.Unlock()
		got[string(m.Value)] = m
		return types.Ack, nil
	}), func() bool { mu.Lock(); defer mu.Unlock(); return len(got) == 2 })

	assert.Equal(t, "q", got[`"v"`].Topic)
	assert.Equal(t, 4, got[`"v"`].DeliveryCount)
	assert.Equal(t, "1b9f6c1e-8c1a-4a9e-9d6e-6f1d2a8c9e10", got["plain"].Id.String())
	f.mu.Lock()
	defer f.mu.Unlock()
	assert.Contains(t, f.lastReceive.MessageSystemAttributeNames, sqstypes.MessageSystemAttributeNameApproximateReceiveCount)
}

func TestConsumer_RejectDeletes(t *testing.T) {
	f := &fakeSQS{messages: []sqstypes.Message{sqsMessage(t, "r-reject", "x")}}
	var handled atomic.Int32
	consumeUntil(t, f, port.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) {
		defer handled.Add(1)
		return types.Reject, nil
	}), func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.deleted) == 1
	})
	assert.Equal(t, []string{"r-reject"}, f.deleted)
}
