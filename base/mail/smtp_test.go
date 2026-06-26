package mail

import (
	"context"
	"crypto/tls"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeServer is a minimal in-memory SMTP server for unit tests.
type fakeServer struct {
	ln net.Listener

	requireAuth bool
	startTLS    *tls.Config // when set, advertise + honor STARTTLS
	badRcpt     map[string]bool

	mu           sync.Mutex
	received     []recvMsg
	sawAuth      bool
	failMailOnce bool
}

type recvMsg struct {
	from string
	to   []string
	data string
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	fs := &fakeServer{ln: ln, badRcpt: map[string]bool{}}
	go fs.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return fs
}

func (fs *fakeServer) port() int { return fs.ln.Addr().(*net.TCPAddr).Port }

func (fs *fakeServer) messages() []recvMsg {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return append([]recvMsg(nil), fs.received...)
}

func (fs *fakeServer) authed() bool {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.sawAuth
}

func (fs *fakeServer) serve() {
	for {
		conn, err := fs.ln.Accept()
		if err != nil {
			return
		}
		go fs.handle(conn)
	}
}

func (fs *fakeServer) handle(conn net.Conn) {
	defer conn.Close()
	tp := textproto.NewConn(conn)
	_ = tp.PrintfLine("220 fake ESMTP")

	var from string
	var to []string
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		up := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(up, "EHLO"), strings.HasPrefix(up, "HELO"):
			_ = tp.PrintfLine("250-fake")
			if fs.startTLS != nil {
				_ = tp.PrintfLine("250-STARTTLS")
			}
			if fs.requireAuth {
				_ = tp.PrintfLine("250-AUTH PLAIN LOGIN")
			}
			_ = tp.PrintfLine("250 OK")
		case strings.HasPrefix(up, "STARTTLS"):
			_ = tp.PrintfLine("220 Ready to start TLS")
			tlsConn := tls.Server(conn, fs.startTLS)
			if err := tlsConn.Handshake(); err != nil {
				return
			}
			conn = tlsConn
			tp = textproto.NewConn(conn)
		case strings.HasPrefix(up, "AUTH PLAIN"):
			fs.mark()
			_ = tp.PrintfLine("235 2.7.0 Authentication successful")
		case strings.HasPrefix(up, "AUTH LOGIN"):
			_ = tp.PrintfLine("334 VXNlcm5hbWU6") // "Username:"
			_, _ = tp.ReadLine()
			_ = tp.PrintfLine("334 UGFzc3dvcmQ6") // "Password:"
			_, _ = tp.ReadLine()
			fs.mark()
			_ = tp.PrintfLine("235 2.7.0 Authentication successful")
		case strings.HasPrefix(up, "MAIL FROM"):
			if fs.takeFailMail() {
				_ = tp.PrintfLine("451 4.3.0 Try again later")
				continue
			}
			from = extractAddr(line)
			_ = tp.PrintfLine("250 OK")
		case strings.HasPrefix(up, "RCPT TO"):
			addr := extractAddr(line)
			if fs.badRcpt[addr] {
				_ = tp.PrintfLine("550 5.1.1 No such user")
				continue
			}
			to = append(to, addr)
			_ = tp.PrintfLine("250 OK")
		case strings.HasPrefix(up, "DATA"):
			_ = tp.PrintfLine("354 End data with <CR><LF>.<CR><LF>")
			data, derr := tp.ReadDotBytes()
			if derr != nil {
				return
			}
			fs.record(from, to, string(data))
			from, to = "", nil
			_ = tp.PrintfLine("250 2.0.0 OK")
		case strings.HasPrefix(up, "RSET"):
			from, to = "", nil
			_ = tp.PrintfLine("250 OK")
		case strings.HasPrefix(up, "QUIT"):
			_ = tp.PrintfLine("221 Bye")
			return
		default:
			_ = tp.PrintfLine("250 OK")
		}
	}
}

func (fs *fakeServer) mark() { fs.mu.Lock(); fs.sawAuth = true; fs.mu.Unlock() }

func (fs *fakeServer) record(from string, to []string, data string) {
	fs.mu.Lock()
	fs.received = append(fs.received, recvMsg{from: from, to: append([]string(nil), to...), data: data})
	fs.mu.Unlock()
}

func (fs *fakeServer) takeFailMail() bool {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.failMailOnce {
		fs.failMailOnce = false
		return true
	}
	return false
}

func extractAddr(line string) string {
	if i := strings.IndexByte(line, '<'); i >= 0 {
		if j := strings.IndexByte(line[i:], '>'); j > 0 {
			return line[i+1 : i+j]
		}
	}
	if k := strings.IndexByte(line, ':'); k >= 0 {
		return strings.TrimSpace(line[k+1:])
	}
	return line
}

func mailerFor(t *testing.T, fs *fakeServer, auth AuthMechanism) Mailer {
	t.Helper()
	m, err := New(Config{
		Host: "localhost", Port: fs.port(),
		From:       Address{Name: "Sender", Email: "f@x.com"},
		Encryption: EncryptionNone, Auth: auth,
		Username: "user", Password: "pass",
		MaxRetries: 2, Timeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

func sampleMsg(to string) *Message {
	return &Message{From: Address{Email: "f@x.com"}, To: []Address{{Email: to}}, Subject: "Hi", Text: "oi", HTML: "<b>oi</b>"}
}

func TestSMTP_Send(t *testing.T) {
	fs := newFakeServer(t)
	m := mailerFor(t, fs, AuthNone)
	if err := m.Send(context.Background(), sampleMsg("a@b.com")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	got := fs.messages()
	if len(got) != 1 || got[0].from != "f@x.com" || got[0].to[0] != "a@b.com" {
		t.Fatalf("unexpected received: %+v", got)
	}
	if !strings.Contains(got[0].data, "Subject:") || !strings.Contains(got[0].data, "multipart/alternative") {
		t.Fatalf("DATA missing expected MIME: %q", got[0].data)
	}
}

func TestSMTP_AuthPlain(t *testing.T) {
	fs := newFakeServer(t)
	fs.requireAuth = true
	m := mailerFor(t, fs, AuthPlain)
	if err := m.Send(context.Background(), sampleMsg("a@b.com")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !fs.authed() {
		t.Fatal("server did not see AUTH PLAIN")
	}
}

func TestSMTP_AuthLogin(t *testing.T) {
	fs := newFakeServer(t)
	fs.requireAuth = true
	m := mailerFor(t, fs, AuthLogin)
	if err := m.Send(context.Background(), sampleMsg("a@b.com")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !fs.authed() {
		t.Fatal("server did not see AUTH LOGIN")
	}
}

func TestSMTP_SendBulk_PartialFailure(t *testing.T) {
	fs := newFakeServer(t)
	fs.badRcpt["bad@b.com"] = true
	m := mailerFor(t, fs, AuthNone)

	msgs := []*Message{
		sampleMsg("ok1@b.com"),
		sampleMsg("bad@b.com"), // RCPT rejected
		sampleMsg("ok2@b.com"),
		{From: Address{Email: "f@x.com"}, To: []Address{{Email: "x@b.com"}}}, // invalid: no body
	}
	res, err := m.SendBulk(context.Background(), msgs)
	if err != nil {
		t.Fatalf("SendBulk batch error: %v", err)
	}
	if res.Sent != 2 {
		t.Fatalf("expected 2 sent, got %d (%+v)", res.Sent, res)
	}
	if len(res.Failed) != 2 {
		t.Fatalf("expected 2 failures, got %d (%+v)", len(res.Failed), res.Failed)
	}
	if len(fs.messages()) != 2 {
		t.Fatalf("server should have received 2 messages, got %d", len(fs.messages()))
	}
}

func TestSMTP_RetryOnTransient(t *testing.T) {
	fs := newFakeServer(t)
	fs.failMailOnce = true // first MAIL FROM → 451, retried
	m := mailerFor(t, fs, AuthNone)
	if err := m.Send(context.Background(), sampleMsg("a@b.com")); err != nil {
		t.Fatalf("Send should succeed after retry: %v", err)
	}
	if len(fs.messages()) != 1 {
		t.Fatalf("expected 1 delivered after retry, got %d", len(fs.messages()))
	}
}

func TestSMTP_ConnectError(t *testing.T) {
	m, err := New(Config{
		Host: "localhost", Port: 1, // nothing listening
		From:       Address{Email: "f@x.com"},
		Encryption: EncryptionNone, Auth: AuthNone,
		MaxRetries: 0, Timeout: 500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := m.Send(context.Background(), sampleMsg("a@b.com")); err == nil {
		t.Fatal("expected a connection error")
	}
}

func TestSMTP_SendBulk_Empty(t *testing.T) {
	fs := newFakeServer(t)
	m := mailerFor(t, fs, AuthNone)
	res, err := m.SendBulk(context.Background(), nil)
	if err != nil || res.Sent != 0 || res.HasFailures() {
		t.Fatalf("empty bulk should be a no-op, got %+v err=%v", res, err)
	}
}
