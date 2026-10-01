package types

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Message is the universal envelope for all broker messages.
//
// Value holds the business payload as raw JSON, making it wire-compatible with
// any broker format and allowing type-safe extraction via UnpackMessage.
// Headers carries cross-cutting metadata (trace-id, account_info, etc.)
// without polluting the business payload.
type Message struct {
	Id        uuid.UUID         `json:"id"`
	Topic     string            `json:"topic,omitempty"`
	Type      string            `json:"type,omitempty"`   // CloudEvents type, e.g. "order.created"
	Source    string            `json:"source,omitempty"` // CloudEvents source; defaults to /topics/<topic>
	Key       string            `json:"key,omitempty"`
	Value     json.RawMessage   `json:"value"`
	Timestamp time.Time         `json:"timestamp"`
	Headers   map[string]string `json:"headers,omitempty"`

	// DeliveryCount is how many times the broker has delivered this message,
	// this delivery included (1 = first). It is set by the consumer from the
	// broker's own counter and is 0 when the broker does not track it. It
	// never travels on the wire.
	DeliveryCount int `json:"-"`
}

// NewMessage creates a Message with the payload serialized as JSON.
// Set Topic via WithTopic or directly before sending.
func NewMessage(value any) (*Message, error) {
	v, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("msq: encode message value: %w", err)
	}
	return &Message{
		Id:        uuid.New(),
		Timestamp: time.Now(),
		Value:     json.RawMessage(v),
		Headers:   make(map[string]string),
	}, nil
}

// NewMessageWithTopic creates a Message with topic and payload already set.
func NewMessageWithTopic(topic string, value any) (*Message, error) {
	msg, err := NewMessage(value)
	if err != nil {
		return nil, err
	}
	msg.Topic = topic
	return msg, nil
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
func (m *Message) DecodeMessage(model any) error {
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
