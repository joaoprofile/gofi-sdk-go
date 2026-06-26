package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// smtpMailer is the SMTP implementation of Mailer.
type smtpMailer struct {
	cfg Config
}

func newSMTPMailer(cfg Config) *smtpMailer { return &smtpMailer{cfg: cfg} }

// Send delivers one message, retrying transient failures with backoff.
func (s *smtpMailer) Send(ctx context.Context, msg *Message) error {
	if err := msg.validate(); err != nil {
		return err
	}
	return s.withRetry(ctx, func() error {
		c, err := s.connect(ctx)
		if err != nil {
			return err
		}
		defer s.quit(c)
		return s.deliver(c, msg)
	})
}

// SendBulk delivers many messages over a reused connection. Per-message failures
// are captured in the result; the error is non-nil only when the batch can't start.
func (s *smtpMailer) SendBulk(ctx context.Context, msgs []*Message) (BulkResult, error) {
	var res BulkResult
	if len(msgs) == 0 {
		return res, nil
	}
	c, err := s.connect(ctx)
	if err != nil {
		return res, err
	}
	defer s.quit(c)

	inBatch := 0
	for i, m := range msgs {
		if verr := m.validate(); verr != nil {
			res.Failed = append(res.Failed, BulkError{Index: i, To: m.recipients(), Err: verr})
			continue
		}

		// Proactively recycle the connection every PoolSize messages.
		if s.cfg.PoolSize > 0 && inBatch >= s.cfg.PoolSize {
			s.quit(c)
			if c, err = s.connect(ctx); err != nil {
				failRemaining(&res, msgs, i, err)
				return res, nil
			}
			inBatch = 0
		}

		if derr := s.deliver(c, m); derr != nil {
			res.Failed = append(res.Failed, BulkError{Index: i, To: m.recipients(), Err: derr})
		} else {
			res.Sent++
			inBatch++
		}

		// Prepare the session for the next message; reconnect if it's broken.
		if c, err = s.recover(ctx, c); err != nil {
			failRemaining(&res, msgs, i+1, err)
			return res, nil
		}
	}
	return res, nil
}

// connect dials and authenticates a fresh client.
func (s *smtpMailer) connect(ctx context.Context) (*smtp.Client, error) {
	c, err := s.dial(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authenticate(c); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// dial opens the transport, applying the configured encryption and deadlines.
func (s *smtpMailer) dial(ctx context.Context) (*smtp.Client, error) {
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	d := &net.Dialer{Timeout: s.cfg.Timeout}

	var conn net.Conn
	var err error
	if s.cfg.Encryption == EncryptionTLS {
		conn, err = tls.DialWithDialer(d, "tcp", addr, s.cfg.tlsConfig())
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, err
	}

	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	} else {
		_ = conn.SetDeadline(time.Now().Add(s.cfg.Timeout))
	}

	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if s.cfg.HELODomain != "" {
		if err := c.Hello(s.cfg.HELODomain); err != nil {
			_ = c.Close()
			return nil, err
		}
	}
	if s.cfg.Encryption == EncryptionSTARTTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			_ = c.Close()
			return nil, fmt.Errorf("%w: server does not advertise STARTTLS", ErrInvalidConfig)
		}
		if err := c.StartTLS(s.cfg.tlsConfig()); err != nil {
			_ = c.Close()
			return nil, err
		}
	}
	return c, nil
}

// authenticate runs SMTP AUTH when a mechanism + credentials are configured and
// the server advertises AUTH.
func (s *smtpMailer) authenticate(c *smtp.Client) error {
	auth := s.cfg.smtpAuth()
	if auth == nil {
		return nil
	}
	if ok, _ := c.Extension("AUTH"); !ok {
		return nil
	}
	return c.Auth(auth)
}

// deliver runs MAIL FROM / RCPT TO / DATA for one message on an open client.
func (s *smtpMailer) deliver(c *smtp.Client, m *Message) error {
	if err := c.Mail(m.From.Email); err != nil {
		return err
	}
	for _, rcpt := range m.recipients() {
		if err := c.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	raw, err := encodeMessage(m)
	if err != nil {
		_ = w.Close()
		return err
	}
	if _, err := w.Write(raw); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

// recover resets the SMTP session for the next message, reconnecting when the
// current connection is no longer usable.
func (s *smtpMailer) recover(ctx context.Context, c *smtp.Client) (*smtp.Client, error) {
	if c != nil && c.Reset() == nil {
		return c, nil
	}
	if c != nil {
		_ = c.Close()
	}
	return s.connect(ctx)
}

func (s *smtpMailer) quit(c *smtp.Client) {
	if c == nil {
		return
	}
	if err := c.Quit(); err != nil {
		_ = c.Close()
	}
}

// withRetry runs fn up to MaxRetries+1 times, backing off between transient failures.
func (s *smtpMailer) withRetry(ctx context.Context, fn func() error) error {
	attempts := s.cfg.MaxRetries + 1
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			backoff := time.Duration(1<<uint(i-1)) * 200 * time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
		if err := fn(); err != nil {
			lastErr = err
			if !isRetryable(err) {
				return err
			}
			continue
		}
		return nil
	}
	return lastErr
}

// smtpAuth maps the configured mechanism to a net/smtp Auth (nil = no auth).
func (c Config) smtpAuth() smtp.Auth {
	switch c.Auth {
	case AuthPlain:
		return smtp.PlainAuth("", c.Username, c.Password, c.Host)
	case AuthCRAMMD5:
		return smtp.CRAMMD5Auth(c.Username, c.Password)
	case AuthLogin:
		return &loginAuth{username: c.Username, password: c.Password}
	default:
		return nil
	}
}

// isRetryable reports whether an error is worth retrying: SMTP 4xx (transient)
// and network errors are; SMTP 5xx (permanent) and config errors are not.
func isRetryable(err error) bool {
	if errors.Is(err, ErrInvalidConfig) {
		return false
	}
	var tp *textproto.Error
	if errors.As(err, &tp) {
		return tp.Code >= 400 && tp.Code < 500
	}
	return true
}

func failRemaining(res *BulkResult, msgs []*Message, from int, err error) {
	for j := from; j < len(msgs); j++ {
		res.Failed = append(res.Failed, BulkError{Index: j, To: msgs[j].recipients(), Err: err})
	}
}

// loginAuth implements the SMTP LOGIN mechanism (not provided by net/smtp).
type loginAuth struct {
	username string
	password string
}

func (a *loginAuth) Start(_ *smtp.ServerInfo) (string, []byte, error) {
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(string(fromServer))) {
	case "username:":
		return []byte(a.username), nil
	case "password:":
		return []byte(a.password), nil
	default:
		return nil, fmt.Errorf("mail: unexpected LOGIN challenge %q", string(fromServer))
	}
}
