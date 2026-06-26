package mail

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/multipart"
	"net/textproto"
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

	writeHeader(&buf, "From", m.From.String())
	writeHeader(&buf, "To", addressList(m.To))
	if len(m.Cc) > 0 {
		writeHeader(&buf, "Cc", addressList(m.Cc))
	}
	writeHeader(&buf, "Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	writeHeader(&buf, "Date", time.Now().Format(time.RFC1123Z))
	writeHeader(&buf, "Message-ID", messageID(m.From.Email))
	writeHeader(&buf, "MIME-Version", "1.0")
	for k, v := range m.Headers {
		writeHeader(&buf, k, v)
	}

	hasAlt := strings.TrimSpace(m.HTML) != "" && strings.TrimSpace(m.Text) != ""

	switch {
	case len(m.Attachments) > 0:
		mw := multipart.NewWriter(&buf)
		writeHeader(&buf, "Content-Type", "multipart/mixed; boundary=\""+mw.Boundary()+"\"")
		buf.WriteString("\r\n")
		if err := writeBody(mw, m, hasAlt); err != nil {
			return nil, err
		}
		for _, att := range m.Attachments {
			if err := writeAttachment(mw, att); err != nil {
				return nil, err
			}
		}
		if err := mw.Close(); err != nil {
			return nil, err
		}

	case hasAlt:
		mw := multipart.NewWriter(&buf)
		writeHeader(&buf, "Content-Type", "multipart/alternative; boundary=\""+mw.Boundary()+"\"")
		buf.WriteString("\r\n")
		if err := writeTextPart(mw, "text/plain", m.Text); err != nil {
			return nil, err
		}
		if err := writeTextPart(mw, "text/html", m.HTML); err != nil {
			return nil, err
		}
		if err := mw.Close(); err != nil {
			return nil, err
		}

	case strings.TrimSpace(m.HTML) != "":
		writeHeader(&buf, "Content-Type", "text/html; charset=\"UTF-8\"")
		writeHeader(&buf, "Content-Transfer-Encoding", "base64")
		buf.WriteString("\r\n")
		buf.WriteString(base64Wrap(m.HTML))

	default:
		writeHeader(&buf, "Content-Type", "text/plain; charset=\"UTF-8\"")
		writeHeader(&buf, "Content-Transfer-Encoding", "base64")
		buf.WriteString("\r\n")
		buf.WriteString(base64Wrap(m.Text))
	}

	return buf.Bytes(), nil
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
	_, err = part.Write([]byte(base64Wrap(body)))
	return err
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
	_, err = part.Write([]byte(base64Wrap(string(att.Content))))
	return err
}

func writeHeader(buf *bytes.Buffer, key, value string) {
	buf.WriteString(key)
	buf.WriteString(": ")
	buf.WriteString(value)
	buf.WriteString("\r\n")
}

func addressList(addrs []Address) string {
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		parts = append(parts, a.String())
	}
	return strings.Join(parts, ", ")
}

// base64Wrap base64-encodes s and folds it into 76-char lines (RFC 2045).
func base64Wrap(s string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(s))
	const lineLen = 76
	var b strings.Builder
	for len(encoded) > lineLen {
		b.WriteString(encoded[:lineLen])
		b.WriteString("\r\n")
		encoded = encoded[lineLen:]
	}
	b.WriteString(encoded)
	return b.String()
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
