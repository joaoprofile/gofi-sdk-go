package kafka

import (
	"context"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/gofi-labs/gofi-sdk-go/msq/port"
	"github.com/gofi-labs/gofi-sdk-go/msq/types"
)

type endedSession struct{ mockSession }

func (*endedSession) Context() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestConsumeClaim_RetryWaitStopsWhenSessionEnds(t *testing.T) {
	h := &groupHandler{
		cfg: types.ConsumeConfig{Topic: "t", MaxRetries: 3, RetryBackoff: time.Hour},
		handler: port.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) {
			return types.Nack, nil
		}),
	}
	claim := &mockClaim{messages: make(chan *sarama.ConsumerMessage, 1)}
	claim.messages <- &sarama.ConsumerMessage{Topic: "t", Value: []byte(`{}`)}
	close(claim.messages)
	sess := &endedSession{}

	done := make(chan error, 1)
	go func() { done <- h.ConsumeClaim(sess, claim) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("retry wait must not block a rebalance")
	}
	if sess.markedCount != 0 {
		t.Fatal("an interrupted record must not be marked")
	}
}
