package mail

import (
	"errors"
	"testing"
)

func TestAddressString(t *testing.T) {
	cases := []struct {
		in   Address
		want string
	}{
		{Address{Email: "a@b.com"}, "a@b.com"},
		{Address{Name: "Ana", Email: "a@b.com"}, "\"Ana\" <a@b.com>"},
	}
	for _, c := range cases {
		if got := c.in.String(); got != c.want {
			t.Errorf("Address.String() = %q, want %q", got, c.want)
		}
	}
}

func TestMessageRecipients(t *testing.T) {
	m := &Message{
		To:  []Address{{Email: "to1@x.com"}, {Email: "to2@x.com"}},
		Cc:  []Address{{Email: "cc@x.com"}},
		Bcc: []Address{{Email: "bcc@x.com"}, {Email: " "}},
	}
	got := m.recipients()
	want := []string{"to1@x.com", "to2@x.com", "cc@x.com", "bcc@x.com"}
	if len(got) != len(want) {
		t.Fatalf("recipients = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("recipients[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMessageValidate(t *testing.T) {
	cases := []struct {
		name string
		msg  *Message
		want error
	}{
		{"no sender", &Message{To: []Address{{Email: "a@b.com"}}, Text: "x"}, ErrNoSender},
		{"no recipients", &Message{From: Address{Email: "f@x.com"}, Text: "x"}, ErrNoRecipients},
		{"empty body", &Message{From: Address{Email: "f@x.com"}, To: []Address{{Email: "a@b.com"}}}, ErrEmptyBody},
		{"ok", &Message{From: Address{Email: "f@x.com"}, To: []Address{{Email: "a@b.com"}}, Text: "x"}, nil},
	}
	for _, c := range cases {
		if err := c.msg.validate(); !errors.Is(err, c.want) {
			t.Errorf("%s: validate() = %v, want %v", c.name, err, c.want)
		}
	}
}

func TestBulkResult(t *testing.T) {
	r := BulkResult{Sent: 2}
	if r.HasFailures() {
		t.Fatal("expected no failures")
	}
	r.Failed = append(r.Failed, BulkError{Index: 3, To: []string{"x@y.com"}, Err: ErrEmptyBody})
	if !r.HasFailures() {
		t.Fatal("expected failures")
	}
	be := r.Failed[0]
	if !errors.Is(be, ErrEmptyBody) {
		t.Fatal("BulkError should unwrap to underlying error")
	}
	if be.Error() == "" {
		t.Fatal("BulkError.Error() should not be empty")
	}
}

func TestNew_InvalidConfig(t *testing.T) {
	if _, err := New(Config{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
	if _, err := New(Config{Host: "smtp.x.com", From: Address{Email: "f@x.com"}}); err != nil {
		t.Fatalf("valid config should build: %v", err)
	}
}

func TestItoa(t *testing.T) {
	for _, c := range []struct {
		n    int
		want string
	}{{0, "0"}, {7, "7"}, {42, "42"}, {-13, "-13"}} {
		if got := itoa(c.n); got != c.want {
			t.Errorf("itoa(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}
