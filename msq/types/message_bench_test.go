package types

import (
	"encoding/json"
	"testing"
)

type benchPayload struct {
	ID     string   `json:"id"`
	Amount float64  `json:"amount"`
	Tags   []string `json:"tags"`
}

func BenchmarkMessageRoundTrip(b *testing.B) {
	p := benchPayload{ID: "ord-123", Amount: 99.9, Tags: []string{"a", "b"}}
	b.ReportAllocs()
	for b.Loop() {
		m := testMessageWithTopic("orders", p).WithKey("ord-123").WithHeader("tenant", "t1")
		body, _ := json.Marshal(m)
		var out Message
		_ = json.Unmarshal(body, &out)
		_, _ = UnpackMessage[benchPayload](&out)
	}
}
