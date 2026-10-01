package mail

import (
	"errors"
	"strings"
	"testing"
)

func validMessage() *Message {
	return &Message{
		From: Address{Email: "from@example.com"},
		To:   []Address{{Email: "to@example.com"}},
		Text: "hi",
	}
}

func TestValidate_RejectsHeaderInjection(t *testing.T) {
	tests := map[string]func(m *Message){
		"CRLF in To email":     func(m *Message) { m.To[0].Email = "to@example.com\r\nBcc: spy@evil.com" },
		"LF in From email":     func(m *Message) { m.From.Email = "from@example.com\nX-Evil: 1" },
		"CRLF in Cc name":      func(m *Message) { m.Cc = []Address{{Name: "A\r\nBcc: x", Email: "cc@example.com"}} },
		"CRLF in header value": func(m *Message) { m.Headers = map[string]string{"X-Tag": "a\r\nBcc: spy@evil.com"} },
		"colon in header name": func(m *Message) { m.Headers = map[string]string{"X-Tag: evil": "v"} },
		"space in header name": func(m *Message) { m.Headers = map[string]string{"X Tag": "v"} },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			m := validMessage()
			mutate(m)
			if err := m.validate(); !errors.Is(err, ErrInvalidHeader) {
				t.Fatalf("err=%v, want ErrInvalidHeader", err)
			}
		})
	}
}

func TestEncodeMessage_CustomHeadersSorted(t *testing.T) {
	m := validMessage()
	m.Headers = map[string]string{"X-B": "2", "X-A": "1", "X-C": "3"}
	for range 5 {
		raw, err := encodeMessage(m)
		if err != nil {
			t.Fatal(err)
		}
		s := string(raw)
		if !(strings.Index(s, "X-A:") < strings.Index(s, "X-B:") && strings.Index(s, "X-B:") < strings.Index(s, "X-C:")) {
			t.Fatalf("headers not sorted:\n%s", s)
		}
	}
}
