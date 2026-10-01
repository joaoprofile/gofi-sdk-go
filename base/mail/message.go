package mail

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"maps"
	"mime"
	"mime/multipart"
	"net/textproto"
	"slices"
	"strings"
	"time"
)

// encodeMessage renders a Message into RFC 5322 / MIME bytes ready for SMTP DATA.
// Layout:
//   - attachments        → multipart/mixed { <body>, attachment* }
//   - HTML+Text (no att) → multipart/alternative { text/plain, text/html }
//   - HTML only          → text/html
//   - Text only          → text/plain
func encodeMessage(m *Message) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeMessage(&buf, m); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeMessage streams the message into w (the SMTP DATA writer), so
// attachments are encoded without holding extra copies in memory.
func writeMessage(w io.Writer, m *Message) error {
	writeHeader(w, "From", m.From.String())
	writeHeader(w, "To", addressList(m.To))
	if len(m.Cc) > 0 {
		writeHeader(w, "Cc", addressList(m.Cc))
	}
	writeHeader(w, "Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	writeHeader(w, "Date", time.Now().Format(time.RFC1123Z))
	writeHeader(w, "Message-ID", messageID(m.From.Email))
	writeHeader(w, "MIME-Version", "1.0")
	for _, k := range slices.Sorted(maps.Keys(m.Headers)) {
		writeHeader(w, k, m.Headers[k])
	}

	hasAlt := strings.TrimSpace(m.HTML) != "" && strings.TrimSpace(m.Text) != ""

	switch {
	case len(m.Attachments) > 0:
		mw := multipart.NewWriter(w)
		writeHeader(w, "Content-Type", "multipart/mixed; boundary=\""+mw.Boundary()+"\"")
		io.WriteString(w, "\r\n")
		if err := writeBody(mw, m, hasAlt); err != nil {
			return err
		}
		for _, att := range m.Attachments {
			if err := writeAttachment(mw, att); err != nil {
				return err
			}
		}
		return mw.Close()

	case hasAlt:
		mw := multipart.NewWriter(w)
		writeHeader(w, "Content-Type", "multipart/alternative; boundary=\""+mw.Boundary()+"\"")
		io.WriteString(w, "\r\n")
		if err := writeTextPart(mw, "text/plain", m.Text); err != nil {
			return err
		}
		if err := writeTextPart(mw, "text/html", m.HTML); err != nil {
			return err
		}
		return mw.Close()

	case strings.TrimSpace(m.HTML) != "":
		writeHeader(w, "Content-Type", "text/html; charset=\"UTF-8\"")
		writeHeader(w, "Content-Transfer-Encoding", "base64")
		io.WriteString(w, "\r\n")
		return writeBase64(w, []byte(m.HTML))

	default:
		writeHeader(w, "Content-Type", "text/plain; charset=\"UTF-8\"")
		writeHeader(w, "Content-Transfer-Encoding", "base64")
		io.WriteString(w, "\r\n")
		return writeBase64(w, []byte(m.Text))
	}
}

// writeBody writes the message body inside a multipart/mixed: either a nested
// multipart/alternative (HTML+text) or a single text/html|text/plain part.
func writeBody(mw *multipart.Writer, m *Message, hasAlt bool) error {
	if hasAlt {
		boundary := randomBoundary()
		head := textproto.MIMEHeader{}
		head.Set("Content-Type", "multipart/alternative; boundary=\""+boundary+"\"")
		part, err := mw.CreatePart(head)
		if err != nil {
			return err
		}
		alt := multipart.NewWriter(part)
		if err := alt.SetBoundary(boundary); err != nil {
			return err
		}
		if err := writeTextPart(alt, "text/plain", m.Text); err != nil {
			return err
		}
		if err := writeTextPart(alt, "text/html", m.HTML); err != nil {
			return err
		}
		return alt.Close()
	}
	if strings.TrimSpace(m.HTML) != "" {
		return writeTextPart(mw, "text/html", m.HTML)
	}
	return writeTextPart(mw, "text/plain", m.Text)
}

func writeTextPart(mw *multipart.Writer, contentType, body string) error {
	head := textproto.MIMEHeader{}
	head.Set("Content-Type", contentType+"; charset=\"UTF-8\"")
	head.Set("Content-Transfer-Encoding", "base64")
	part, err := mw.CreatePart(head)
	if err != nil {
		return err
	}
	return writeBase64(part, []byte(body))
}

func writeAttachment(mw *multipart.Writer, att Attachment) error {
	ct := att.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	head := textproto.MIMEHeader{}
	head.Set("Content-Type", ct)
	head.Set("Content-Transfer-Encoding", "base64")
	head.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", att.Filename))
	part, err := mw.CreatePart(head)
	if err != nil {
		return err
	}
	return writeBase64(part, att.Content)
}

// writeHeader ignores write errors: the writer's next body write reports them.
func writeHeader(w io.Writer, key, value string) {
	io.WriteString(w, key)
	io.WriteString(w, ": ")
	io.WriteString(w, value)
	io.WriteString(w, "\r\n")
}

func addressList(addrs []Address) string {
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		parts = append(parts, a.String())
	}
	return strings.Join(parts, ", ")
}

// base64LineLen is the RFC 2045 line limit for base64 bodies.
const base64LineLen = 76

var crlf = []byte("\r\n")

// writeBase64 streams data as base64 folded into 76-char CRLF lines.
func writeBase64(w io.Writer, data []byte) error {
	enc := base64.NewEncoder(base64.StdEncoding, &foldWriter{w: w})
	if _, err := enc.Write(data); err != nil {
		return err
	}
	return enc.Close()
}

// foldWriter inserts CRLF every base64LineLen bytes, never after the last line.
type foldWriter struct {
	w   io.Writer
	col int
}

func (f *foldWriter) Write(p []byte) (int, error) {
	n := 0
	for len(p) > 0 {
		if f.col == base64LineLen {
			if _, err := f.w.Write(crlf); err != nil {
				return n, err
			}
			f.col = 0
		}
		chunk := min(len(p), base64LineLen-f.col)
		m, err := f.w.Write(p[:chunk])
		n += m
		f.col += m
		if err != nil {
			return n, err
		}
		p = p[chunk:]
	}
	return n, nil
}

func randomBoundary() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func messageID(fromEmail string) string {
	return fmt.Sprintf("<%s@%s>", randomBoundary(), domainOf(fromEmail))
}

func domainOf(email string) string {
	if i := strings.LastIndexByte(email, '@'); i >= 0 && i < len(email)-1 {
		return email[i+1:]
	}
	return "localhost"
}
