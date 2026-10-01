package types

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBinding_RoundTrip(t *testing.T) {
	for name, b := range map[string]Binding{"kafka": KafkaBinding, "amqp": AMQPBinding} {
		t.Run(name, func(t *testing.T) {
			in := &Message{
				Id: uuid.New(), Topic: "orders", Key: "k1", Type: "order.created", Source: "/billing",
				Value:     json.RawMessage(`{"id":1}`),
				Timestamp: time.Date(2026, 9, 26, 10, 0, 0, 123, time.UTC),
				Headers:   map[string]string{"traceparent": "00-abc", "tenant": "a"},
			}
			body, headers := b.Encode(in)
			if !b.IsBinary(headers) {
				t.Fatal("encoded headers must be detected as binary mode")
			}
			out := b.Decode(body, headers)
			if out.Id != in.Id || out.Type != in.Type || out.Source != in.Source || !out.Timestamp.Equal(in.Timestamp) {
				t.Errorf("attributes lost: %+v", out)
			}
			if string(out.Value) != `{"id":1}` {
				t.Errorf("Value=%s", out.Value)
			}
			if len(out.Headers) != 2 || out.Headers["traceparent"] != "00-abc" || out.Headers["tenant"] != "a" {
				t.Errorf("Headers=%v, want only user headers", out.Headers)
			}
			if b.KeyInHeaders && out.Key != "k1" {
				t.Errorf("Key=%q", out.Key)
			}
			if len(in.Headers) != 2 {
				t.Error("Encode must not mutate the message headers")
			}
		})
	}
}

func TestBinding_EncodeDefaults(t *testing.T) {
	_, h := KafkaBinding.Encode(&Message{Id: uuid.New(), Topic: "orders"})
	if h["ce_type"] != DefaultEventType || h["ce_source"] != "/topics/orders" || h["content-type"] != "application/json" {
		t.Errorf("defaults not applied: %v", h)
	}
	if _, ok := h["ce_time"]; ok {
		t.Error("zero timestamp must not be encoded")
	}
}

func TestBinding_DecodeForeignID(t *testing.T) {
	h := map[string]string{"ce_specversion": "1.0", "ce_id": "A234-1234", "ce_type": "x", "ce_source": "/y", "ce_ext": "v"}
	a := KafkaBinding.Decode(nil, h)
	b := KafkaBinding.Decode(nil, h)
	if a.Id == uuid.Nil || a.Id != b.Id {
		t.Errorf("non-UUID ids must map to a stable UUID: %s %s", a.Id, b.Id)
	}
	if a.Headers["ce_ext"] != "v" {
		t.Error("unknown extensions must stay in Headers")
	}
}

func TestDecodeEnvelope(t *testing.T) {
	id := uuid.New()
	env, _ := json.Marshal(Message{Id: id, Topic: "orders", Value: json.RawMessage(`"v"`)})
	if m := DecodeEnvelope(env); m.Id != id || m.Topic != "orders" {
		t.Errorf("envelope not decoded: %+v", m)
	}
	raw := DecodeEnvelope([]byte(`{"foreign":true}`))
	if string(raw.Value) != `{"foreign":true}` || raw.Id != uuid.Nil {
		t.Errorf("foreign body must be delivered raw with a zero id for the provider to fill: %+v", raw)
	}
	if m := DecodeEnvelope([]byte("not json")); string(m.Value) != "not json" {
		t.Errorf("invalid JSON must be delivered raw: %+v", m)
	}
}

func TestStableID(t *testing.T) {
	u := uuid.New()
	if StableID(u.String()) != u {
		t.Error("a UUID transport id must be kept")
	}
	a, b := StableID("orders/3/42"), StableID("orders/3/42")
	if a != b || a == uuid.Nil || a == StableID("orders/3/43") {
		t.Errorf("non-UUID ids must map to a stable, distinct UUID: %s %s", a, b)
	}
}

func TestDeliveryCountNotOnWire(t *testing.T) {
	m := Message{Id: uuid.New(), Value: json.RawMessage(`1`), DeliveryCount: 3}
	b, _ := json.Marshal(m)
	if got := DecodeEnvelope(b); got.DeliveryCount != 0 {
		t.Errorf("DeliveryCount must not travel in the envelope: %s", b)
	}
}
