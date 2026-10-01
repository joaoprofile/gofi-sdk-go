package rabbitmq

import (
	"errors"
	"sync"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// amqp091 hands the Return to the listener before it dispatches the ack, so a
// Return still buffered when the ack is seen must fail that publish.
func TestReturnTrackerFailsReturnedPublish(t *testing.T) {
	returns := make(chan amqp.Return, 4)
	tr := newReturnTracker(returns)
	p := tr.track("id-1")
	other := tr.track("id-2")

	returns <- amqp.Return{MessageId: "id-1", Exchange: "ex", RoutingKey: "orders", ReplyCode: 312, ReplyText: "NO_ROUTE"}
	err := tr.result(p)
	require.ErrorIs(t, err, ErrUnroutable)
	assert.ErrorContains(t, err, `routing key "orders"`)
	assert.NoError(t, tr.result(other), "only the returned publish fails")

	tr.untrack(p)
	tr.untrack(other)
	assert.Empty(t, tr.pending, "settled publishes are forgotten")
	close(returns)
}

// Two in-flight publishes with the same MessageId: each Return settles one.
func TestReturnTrackerMatchesDuplicateIDsInOrder(t *testing.T) {
	returns := make(chan amqp.Return)
	tr := newReturnTracker(returns)
	a, b := tr.track("dup"), tr.track("dup")
	returns <- amqp.Return{MessageId: "dup"}
	assert.Error(t, tr.result(a))
	assert.NoError(t, tr.result(b))
	close(returns)
}

func TestReturnTrackerAfterChannelClose(t *testing.T) {
	returns := make(chan amqp.Return, 1)
	tr := newReturnTracker(returns)
	p := tr.track("id")
	returns <- amqp.Return{MessageId: "id"}
	close(returns)
	<-tr.stopped
	assert.True(t, errors.Is(tr.result(p), ErrUnroutable), "Returns received before the close still count")
	assert.NoError(t, tr.result(tr.track("late")))
}

func TestReturnTrackerConcurrentResults(t *testing.T) {
	returns := make(chan amqp.Return, 64)
	tr := newReturnTracker(returns)
	var wg sync.WaitGroup
	for i := range 50 {
		p := tr.track(string(rune('a' + i%26)))
		wg.Go(func() {
			_ = tr.result(p)
			tr.untrack(p)
		})
	}
	wg.Wait()
	close(returns)
}

func TestNilReturnTrackerIsNoop(t *testing.T) {
	var tr *returnTracker
	tr.untrack(nil)
	assert.NoError(t, tr.result(nil))
}
