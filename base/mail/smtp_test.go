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
	dataDelay   time.Duration // simulates a slow server per message

	mu           sync.Mutex
	received     []recvMsg
	sawAuth      bool
	failMailOnce bool
	conns, quits int
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

// sessions returns how many connections were accepted and how many ended
// with QUIT.
func (fs *fakeServer) sessions() (conns, quits int) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.conns, fs.quits
}

func (fs *fakeServer) serve() {
	for {
		conn, err := fs.ln.Accept()
		if err != nil {
			return
		}
		fs.mu.Lock()
		fs.conns++
		fs.mu.Unlock()
		go fs.handle(conn)
	}
}

// fakeSession is one client connection to the fake server.
type fakeSession struct {
	fs   *fakeServer
	conn net.Conn
	tp   *textproto.Conn
	from string
	to   []string
}

func (fs *fakeServer) handle(conn net.Conn) {
	s := &fakeSession{fs: fs, conn: conn, tp: textproto.NewConn(conn)}
	defer func() { _ = s.conn.Close() }()
	_ = s.tp.PrintfLine("220 fake ESMTP")
	for {
		line, err := s.tp.ReadLine()
		if err != nil || !s.command(line) {
			return
		}
	}
}

// command answers one SMTP command; false ends the session.
func (s *fakeSession) command(line string) bool {
	up := strings.ToUpper(line)
	switch {
	case strings.HasPrefix(up, "EHLO"), strings.HasPrefix(up, "HELO"):
		s.ehlo()
	case strings.HasPrefix(up, "STARTTLS"):
		return s.startTLS()
	case strings.HasPrefix(up, "AUTH PLAIN"):
		s.fs.mark()
		_ = s.tp.PrintfLine("235 2.7.0 Authentication successful")
	case strings.HasPrefix(up, "AUTH LOGIN"):
		s.authLogin()
	case strings.HasPrefix(up, "MAIL FROM"):
		s.mailFrom(line)
	case strings.HasPrefix(up, "RCPT TO"):
		s.rcptTo(line)
	case strings.HasPrefix(up, "DATA"):
		return s.data()
	case strings.HasPrefix(up, "RSET"):
		s.from, s.to = "", nil
		_ = s.tp.PrintfLine("250 OK")
	case strings.HasPrefix(up, "QUIT"):
		s.fs.mu.Lock()
		s.fs.quits++
		s.fs.mu.Unlock()
		_ = s.tp.PrintfLine("221 Bye")
		return false
	default:
		_ = s.tp.PrintfLine("250 OK")
	}
	return true
}

func (s *fakeSession) ehlo() {
	_ = s.tp.PrintfLine("250-fake")
	if s.fs.startTLS != nil {
		_ = s.tp.PrintfLine("250-STARTTLS")
	}
	if s.fs.requireAuth {
		_ = s.tp.PrintfLine("250-AUTH PLAIN LOGIN")
	}
	_ = s.tp.PrintfLine("250 OK")
}

func (s *fakeSession) startTLS() bool {
	_ = s.tp.PrintfLine("220 Ready to start TLS")
	tlsConn := tls.Server(s.conn, s.fs.startTLS)
	if err := tlsConn.Handshake(); err != nil {
		return false
	}
	s.conn = tlsConn
	s.tp = textproto.NewConn(tlsConn)
	return true
}

func (s *fakeSession) authLogin() {
	_ = s.tp.PrintfLine("334 VXNlcm5hbWU6") // "Username:"
	_, _ = s.tp.ReadLine()
	_ = s.tp.PrintfLine("334 UGFzc3dvcmQ6") // "Password:"
	_, _ = s.tp.ReadLine()
	s.fs.mark()
	_ = s.tp.PrintfLine("235 2.7.0 Authentication successful")
}

func (s *fakeSession) mailFrom(line string) {
	if s.fs.takeFailMail() {
		_ = s.tp.PrintfLine("451 4.3.0 Try again later")
		return
	}
	s.from = extractAddr(line)
	_ = s.tp.PrintfLine("250 OK")
}

func (s *fakeSession) rcptTo(line string) {
	addr := extractAddr(line)
	if s.fs.badRcpt[addr] {
		_ = s.tp.PrintfLine("550 5.1.1 No such user")
		return
	}
	s.to = append(s.to, addr)
	_ = s.tp.PrintfLine("250 OK")
}

func (s *fakeSession) data() bool {
	_ = s.tp.PrintfLine("354 End data with <CR><LF>.<CR><LF>")
	data, err := s.tp.ReadDotBytes()
	if err != nil {
		return false
	}
	s.fs.record(s.from, s.to, string(data))
	s.from, s.to = "", nil
	time.Sleep(s.fs.dataDelay)
	_ = s.tp.PrintfLine("250 2.0.0 OK")
	return true
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
	if _, after, ok := strings.Cut(line, ":"); ok {
		return strings.TrimSpace(after)
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

// Regression: the deferred QUIT used to target the first connection, so the
// last one opened by a reconnect was never closed.
func TestSMTP_SendBulk_QuitsEveryConnection(t *testing.T) {
	fs := newFakeServer(t)
	m, err := New(Config{
		Host: "localhost", Port: fs.port(),
		From:       Address{Email: "f@x.com"},
		Encryption: EncryptionNone, Auth: AuthNone,
		PoolSize: 1, Timeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := m.SendBulk(context.Background(), []*Message{sampleMsg("a@x.com"), sampleMsg("b@x.com"), sampleMsg("c@x.com")})
	if err != nil || res.Sent != 3 {
		t.Fatalf("SendBulk=%+v,%v", res, err)
	}
	if conns, quits := fs.sessions(); conns != 3 || quits != 3 {
		t.Errorf("conns=%d quits=%d; want 3 and 3", conns, quits)
	}
}
