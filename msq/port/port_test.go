package port_test

import (
	"context"
	"errors"
	"testing"

	"github.com/joaoprofile/gofi/msq/port"
	"github.com/joaoprofile/gofi/msq/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MessageHandlerFunc

func TestMessageHandlerFuncImplementsInterface(t *testing.T) {
	var _ port.MessageHandler = port.MessageHandlerFunc(nil)
}

func TestMessageHandlerFuncHandle(t *testing.T) {
	called := false
	var capturedMsg *types.Message

	fn := port.MessageHandlerFunc(func(ctx context.Context, msg *types.Message) (types.Result, error) {
		called = true
		capturedMsg = msg
		return types.Ack, nil
	})

	msg := types.NewMessageWithTopic("t", "payload")
	result, err := fn.Handle(context.Background(), msg)

	require.NoError(t, err)
	assert.Equal(t, types.Ack, result)
	assert.True(t, called)
	assert.Same(t, msg, capturedMsg)
}

func TestMessageHandlerFuncHandleNack(t *testing.T) {
	sentinel := errors.New("processing failed")

	fn := port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
		return types.Nack, sentinel
	})

	result, err := fn.Handle(context.Background(), &types.Message{})
	assert.Equal(t, types.Nack, result)
	assert.ErrorIs(t, err, sentinel)
}

func TestMessageHandlerFuncHandleIgnore(t *testing.T) {
	fn := port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
		return types.Ignore, nil
	})

	result, err := fn.Handle(context.Background(), &types.Message{})
	assert.Equal(t, types.Ignore, result)
	assert.NoError(t, err)
}

func TestMessageHandlerFuncForwardsContext(t *testing.T) {
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "sentinel-value")

	var gotCtx context.Context
	fn := port.MessageHandlerFunc(func(c context.Context, _ *types.Message) (types.Result, error) {
		gotCtx = c
		return types.Ack, nil
	})

	fn.Handle(ctx, &types.Message{}) //nolint:errcheck
	assert.Equal(t, "sentinel-value", gotCtx.Value(ctxKey{}))
}
