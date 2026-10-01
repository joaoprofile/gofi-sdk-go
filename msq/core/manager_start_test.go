package core_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gofi-labs/gofi-sdk-go/msq/core"
	"github.com/gofi-labs/gofi-sdk-go/msq/port"
	"github.com/gofi-labs/gofi-sdk-go/msq/types"
	"github.com/stretchr/testify/assert"
)

type countingBroker struct {
	fakeBroker
	created atomic.Int32
}

func (b *countingBroker) NewConsumer(types.ConsumeConfig) (port.Consumer, error) {
	b.created.Add(1)
	return &fakeConsumer{}, nil
}

func ack(context.Context, *types.Message) (types.Result, error) { return types.Ack, nil }

func TestConsumerManager_StartOnlyLaunchesNewEntries(t *testing.T) {
	b := &countingBroker{}
	m := core.NewConsumerManager(b)
	defer m.Close()

	m.Register(types.ConsumeConfig{Topic: "a"}, ack)
	assert.NoError(t, m.Start())
	assert.NoError(t, m.Start())
	assert.Equal(t, int32(1), b.created.Load(), "a second Start must not duplicate consumers")

	m.Register(types.ConsumeConfig{Topic: "b"}, ack)
	assert.NoError(t, m.Start())
	assert.Equal(t, int32(2), b.created.Load(), "entries registered later still start")
}

func TestConsumerManager_ConcurrentRegisterAndStart(t *testing.T) {
	m := core.NewConsumerManager(&countingBroker{})
	defer m.Close()
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { m.Register(types.ConsumeConfig{Topic: "t"}, ack) })
		wg.Go(func() { _ = m.Start() })
	}
	wg.Wait()
}

func TestConsumerManager_StartReportsCreationErrors(t *testing.T) {
	m := core.NewConsumerManager(&nilConsumerBroker{})
	defer m.Close()
	m.Register(types.ConsumeConfig{Topic: "a"}, ack)
	m.Register(types.ConsumeConfig{}, ack)
	err := m.Start()
	assert.ErrorIs(t, err, core.ErrConsumerFailed)
	assert.ErrorIs(t, err, core.ErrTopicRequired)
}
