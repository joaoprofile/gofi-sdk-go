package port_test

import (
	"github.com/joaoprofile/gofi-sdk-go/msq/types"
)

// testMessage builds a message for tests; payloads here always encode.
func testMessage(v any) *types.Message {
	m, err := types.NewMessage(v)
	if err != nil {
		panic(err)
	}
	return m
}

func testMessageWithTopic(topic string, v any) *types.Message {
	return testMessage(v).WithTopic(topic)
}
