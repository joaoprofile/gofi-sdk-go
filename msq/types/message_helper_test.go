package types

// testMessage builds a message for tests; payloads here always encode.
func testMessage(v any) *Message {
	m, err := NewMessage(v)
	if err != nil {
		panic(err)
	}
	return m
}

func testMessageWithTopic(topic string, v any) *Message {
	return testMessage(v).WithTopic(topic)
}
