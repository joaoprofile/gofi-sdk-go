package types

import (
	"cmp"
	"encoding/json"
	"maps"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Encoding selects how a provider writes messages on the wire.
type Encoding string

const (
	// EncodingEnvelope writes the whole Message as JSON in the body. It is the
	// default and what every gofi version reads.
	EncodingEnvelope Encoding = "envelope"
	// EncodingCloudEvents writes CloudEvents 1.0 binary mode: the body is Value
	// and the attributes travel in native headers, readable by any CloudEvents
	// SDK. Enable it once every consumer runs gofi v0.5 or later.
	EncodingCloudEvents Encoding = "cloudevents"
)

// Default CloudEvents attributes for messages that do not set them.
const (
	DefaultEventType = "gofi.message"
	contentTypeJSON  = "application/json"
)

// CloudEvents binary-mode bindings.
var (
	// KafkaBinding follows the CloudEvents Kafka protocol binding.
	KafkaBinding = Binding{Prefix: "ce_", ContentType: "content-type"}
	// AMQPBinding follows the CloudEvents AMQP binding; the content type goes
	// in the AMQP property instead of a header.
	AMQPBinding = Binding{Prefix: "cloudEvents_", KeyInHeaders: true}
	// NATSBinding follows the CloudEvents NATS binding (headers like HTTP).
	NATSBinding = Binding{Prefix: "ce-", ContentType: "content-type", KeyInHeaders: true}
)

// ceNamespace derives stable message IDs from non-UUID CloudEvents ids.
var ceNamespace = uuid.MustParse("5b0a3f3e-3c55-4b1e-9f6e-6f1d2a8c9e10")

// Binding maps a Message to CloudEvents binary mode for one transport.
type Binding struct {
	// Prefix of the attribute headers (ce_, cloudEvents_).
	Prefix string
	// ContentType is the header carrying datacontenttype; empty when the
	// transport has a native property for it.
	ContentType string
	// KeyInHeaders writes Message.Key as the partitionkey extension.
	KeyInHeaders bool
}

// Encode returns the body and headers of m. User headers are kept as is.
func (b Binding) Encode(m *Message) ([]byte, map[string]string) {
	h := make(map[string]string, len(m.Headers)+7)
	maps.Copy(h, m.Headers)
	h[b.Prefix+"specversion"] = "1.0"
	h[b.Prefix+"id"] = m.Id.String()
	h[b.Prefix+"type"] = cmp.Or(m.Type, DefaultEventType)
	h[b.Prefix+"source"] = cmp.Or(m.Source, "/topics/"+m.Topic)
	if !m.Timestamp.IsZero() {
		h[b.Prefix+"time"] = m.Timestamp.UTC().Format(time.RFC3339Nano)
	}
	if b.ContentType != "" {
		h[b.ContentType] = contentTypeJSON
	}
	if b.KeyInHeaders && m.Key != "" {
		h[b.Prefix+"partitionkey"] = m.Key
	}
	return m.Value, h
}

// IsBinary reports whether headers carry a CloudEvents binary-mode message.
func (b Binding) IsBinary(headers map[string]string) bool {
	_, ok := headers[b.Prefix+"specversion"]
	return ok
}

// Decode rebuilds a Message from a binary-mode body and headers. CloudEvents
// attributes are removed from Headers; a non-UUID id maps to a stable UUID.
func (b Binding) Decode(body []byte, headers map[string]string) Message {
	m := Message{Value: json.RawMessage(body)}
	rest := make(map[string]string, len(headers))
	for k, v := range headers {
		name, ok := strings.CutPrefix(k, b.Prefix)
		if !ok {
			if k != b.ContentType || b.ContentType == "" {
				rest[k] = v
			}
			continue
		}
		switch name {
		case "id":
			m.Id = parseID(v)
		case "type":
			m.Type = v
		case "source":
			m.Source = v
		case "time":
			m.Timestamp, _ = time.Parse(time.RFC3339Nano, v)
		case "partitionkey":
			m.Key = v
		case "specversion", "datacontenttype":
		default:
			rest[k] = v // unknown extensions stay visible to handlers
		}
	}
	if len(rest) > 0 {
		m.Headers = rest
	}
	if m.Type == DefaultEventType {
		m.Type = ""
	}
	return m
}

// DecodeEnvelope reads a JSON envelope. Bodies that are not an envelope
// (foreign producers) are delivered as the raw Value with a zero Id: the
// provider sets one from the transport (see StableID) so that it stays the
// same across redeliveries.
func DecodeEnvelope(body []byte) Message {
	var m Message
	if err := json.Unmarshal(body, &m); err != nil || len(m.Value) == 0 {
		return Message{Value: json.RawMessage(body), Timestamp: time.Now()}
	}
	return m
}

// StableID maps a transport message id (SQS MessageId, AMQP message-id,
// Kafka topic/partition/offset, ...) to a UUID: a UUID string is kept, any
// other value maps to the same name-based UUID every time.
func StableID(s string) uuid.UUID { return parseID(s) }

func parseID(s string) uuid.UUID {
	if id, err := uuid.Parse(s); err == nil {
		return id
	}
	return uuid.NewSHA1(ceNamespace, []byte(s))
}
