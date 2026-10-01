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
)

type failingGroup struct {
	mockConsumerGroup
	calls atomic.Int32
}

func (g *failingGroup) Consume(context.Context, []string, sarama.ConsumerGroupHandler) error {
	g.calls.Add(1)
	return errors.New("broker down")
}

func TestKafkaConsumer_BacksOffOnSessionErrors(t *testing.T) {
	g := &failingGroup{}
	c := &kafkaConsumer{
		cfg:      types.ConsumeConfig{Topic: "topic"},
		newGroup: func() (sarama.ConsumerGroup, error) { return g, nil },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	_ = c.Consume(ctx, port.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) {
		return types.Ack, nil
	}))
	if n := g.calls.Load(); n > 10 {
		t.Fatalf("group.Consume called %d times in 300ms: no backoff", n)
	}
}
