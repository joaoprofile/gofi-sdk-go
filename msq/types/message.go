package types

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Encoder encodes a payload to bytes for broker transmission.
// Used by byte-oriented brokers such as Kafka.
type Encoder interface {
	Encode() ([]byte, error)
	Length() int
}

// ByteEncoder implements Encoder for raw byte payloads.
type ByteEncoder []byte

func (b ByteEncoder) Encode() ([]byte, error) { return b, nil }
func (b ByteEncoder) Length() int             { return len(b) }

// Message is the universal envelope for all broker messages.
//
// Value holds the business payload as raw JSON, making it wire-compatible with
// any broker format and allowing type-safe extraction via UnpackMessage.
// Headers carries cross-cutting metadata (trace-id, account_info, etc.)
// without polluting the business payload.
type Message struct {
	Id        uuid.UUID         `json:"id"`
	Topic     string            `json:"topic,omitempty"`
	Key       string            `json:"key,omitempty"`
	Value     json.RawMessage   `json:"value"`
	Timestamp time.Time         `json:"timestamp"`
	Headers   map[string]string `json:"headers,omitempty"`
}

// NewMessage creates a Message with the payload serialized as JSON.
// Set Topic via WithTopic or directly before sending.
func NewMessage(value interface{}) *Message {
	v, _ := json.Marshal(value)
	return &Message{
		Id:        uuid.New(),
		Timestamp: time.Now(),
		Value:     json.RawMessage(v),
		Headers:   make(map[string]string),
	}
}

// NewMessageWithTopic creates a Message with topic and payload already set.
func NewMessageWithTopic(topic string, value interface{}) *Message {
	msg := NewMessage(value)
	msg.Topic = topic
	return msg
}

// WithTopic sets the routing topic and returns the message for chaining.
func (m *Message) WithTopic(topic string) *Message {
	m.Topic = topic
	return m
}

// WithKey sets the partition or routing key and returns the message for chaining.
func (m *Message) WithKey(key string) *Message {
	m.Key = key
	return m
}

// WithHeader adds a metadata header and returns the message for chaining.
func (m *Message) WithHeader(key, val string) *Message {
	if m.Headers == nil {
		m.Headers = make(map[string]string)
	}
	m.Headers[key] = val
	return m
}

// String returns the JSON representation of the message.
func (m *Message) String() string {
	b, _ := json.Marshal(m)
	return string(b)
}

// DecodeMessage decodes Value into the given model pointer.
func (m *Message) DecodeMessage(model interface{}) error {
	return json.NewDecoder(bytes.NewReader(m.Value)).Decode(model)
}

// UnpackMessage decodes Value into T using type inference.
func UnpackMessage[T any](message *Message) (*T, error) {
	var result T
	if err := json.Unmarshal(message.Value, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
