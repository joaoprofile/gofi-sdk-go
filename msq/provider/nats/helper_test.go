package nats_test

import (
	"context"
	"sync"

	"github.com/joaoprofile/gofi-sdk-go/msq/port"
	"github.com/joaoprofile/gofi-sdk-go/msq/types"
)

var handleMu sync.Mutex

func portFunc(f func(*types.Message)) port.MessageHandler {
	return port.MessageHandlerFunc(func(_ context.Context, m *types.Message) (types.Result, error) {
		handleMu.Lock()
		defer handleMu.Unlock()
		f(m)
		return types.Ack, nil
	})
}
