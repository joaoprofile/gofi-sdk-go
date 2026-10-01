package msq_test

import (
	"github.com/joaoprofile/gofi-sdk-go/msq"
)

// testMessage builds a message for tests; payloads here always encode.
func testMessage(v any) *msq.Message {
	m, err := msq.NewMessage(v)
	if err != nil {
		panic(err)
	}
	return m
}

func testMessageWithTopic(topic string, v any) *msq.Message {
	return testMessage(v).WithTopic(topic)
}
