// Package mail sends transactional and bulk e-mail over any SMTP provider, with
// HTML + plain-text bodies, attachments and HTML templates. It is configurable
// via the environment (MAIL_*) and built to be robust: TLS/STARTTLS, multiple
// SMTP AUTH mechanisms, timeouts, retry with backoff, and connection reuse for
// bulk sends with per-message error reporting.
//
//	m, err := mail.NewFromEnv()
//	err = m.Send(ctx, &mail.Message{
//	    From:    mail.Address{Name: "BlueFamly", Email: "no-reply@bluefamly.app"},
//	    To:      []mail.Address{{Email: "user@example.com"}},
//	    Subject: "Olá",
//	    HTML:    "<h1>Oi</h1>",
//	    Text:    "Oi",
//	})
package mail

import (
	"context"
	"errors"
	"net/mail"
	"strings"
)

// Errors returned by the package.
var (
	ErrNoRecipients  = errors.New("mail: message has no recipients")
	ErrNoSender      = errors.New("mail: message has no From address")
	ErrEmptyBody     = errors.New("mail: message has neither HTML nor text body")
	ErrNotConfigured = errors.New("mail: SMTP is not configured (MAIL_HOST/MAIL_FROM_EMAIL)")
	ErrInvalidConfig = errors.New("mail: invalid configuration")
)

// Address is an e-mail address with an optional display name.
type Address struct {
	Name  string
	Email string
}

// String renders the address as a header value, RFC 5322-encoding the name when
// needed (e.g. `"João" <joao@example.com>`).
func (a Address) String() string {
	if a.Name == "" {
		return a.Email
	}
	return (&mail.Address{Name: a.Name, Address: a.Email}).String()
}

// Attachment is a file attached to a message.
type Attachment struct {
	Filename    string
	ContentType string // defaults to application/octet-stream
	Content     []byte
}

// Message is an e-mail to be delivered. At least one recipient, a From address
// and one body (HTML or Text) are required. When both HTML and Text are set the
// message is sent as multipart/alternative.
type Message struct {
	From        Address
	To          []Address
	Cc          []Address
	Bcc         []Address
	Subject     string
	HTML        string
	Text        string
	Attachments []Attachment
	Headers     map[string]string
}

// recipients returns every destination address (To + Cc + Bcc) for the SMTP
// envelope (RCPT TO).
func (m *Message) recipients() []string {
	out := make([]string, 0, len(m.To)+len(m.Cc)+len(m.Bcc))
	for _, group := range [][]Address{m.To, m.Cc, m.Bcc} {
		for _, a := range group {
			if e := strings.TrimSpace(a.Email); e != "" {
				out = append(out, e)
			}
		}
	}
	return out
}

// validate checks the minimum contract before delivery.
func (m *Message) validate() error {
	if strings.TrimSpace(m.From.Email) == "" {
		return ErrNoSender
	}
	if len(m.recipients()) == 0 {
		return ErrNoRecipients
	}
	if strings.TrimSpace(m.HTML) == "" && strings.TrimSpace(m.Text) == "" {
		return ErrEmptyBody
	}
	return nil
}

// BulkError describes the failure of a single message within a bulk send.
type BulkError struct {
	Index int
	To    []string
	Err   error
}

func (e BulkError) Error() string {
	return "mail: bulk message " + itoa(e.Index) + " failed: " + e.Err.Error()
}

func (e BulkError) Unwrap() error { return e.Err }

// BulkResult reports the outcome of SendBulk: how many were delivered and which
// ones failed (one bad recipient does not abort the whole batch).
type BulkResult struct {
	Sent   int
	Failed []BulkError
}

// HasFailures reports whether any message in the batch failed.
func (r BulkResult) HasFailures() bool { return len(r.Failed) > 0 }

// Mailer delivers messages. Implemented by the SMTP mailer; mockable in tests.
type Mailer interface {
	// Send delivers a single message (with retry/backoff on transient failures).
	Send(ctx context.Context, msg *Message) error
	// SendBulk delivers many messages reusing the connection, capturing per-message
	// failures in the result. The returned error is non-nil only when the batch
	// could not start at all (e.g. cannot connect/authenticate).
	SendBulk(ctx context.Context, msgs []*Message) (BulkResult, error)
}

// New builds a Mailer from an explicit Config.
func New(cfg Config) (Mailer, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return newSMTPMailer(cfg), nil
}

// NewFromEnv builds a Mailer from the environment (MAIL_*). Returns
// ErrNotConfigured when the minimum settings are absent.
func NewFromEnv() (Mailer, error) {
	cfg, err := FromEnv()
	if err != nil {
		return nil, err
	}
	return New(cfg)
}

// itoa avoids importing strconv just for error strings.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
