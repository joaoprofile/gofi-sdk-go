package mail

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
)

func parseBack(t *testing.T, raw []byte) (*mail.Message, string, map[string]string) {
	t.Helper()
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	ct := msg.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil {
		t.Fatalf("ParseMediaType(%q): %v", ct, err)
	}
	parts := map[string]string{}
	if strings.HasPrefix(mediaType, "multipart/") {
		collectParts(t, msg.Body, params["boundary"], parts)
	} else {
		parts[mediaType] = decodeBody(t, msg.Header.Get("Content-Transfer-Encoding"), msg.Body)
	}
	return msg, mediaType, parts
}

func collectParts(t *testing.T, body io.Reader, boundary string, out map[string]string) {
	t.Helper()
	mr := multipart.NewReader(body, boundary)
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("NextPart: %v", err)
		}
		mt, params, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		if strings.HasPrefix(mt, "multipart/") {
			collectParts(t, p, params["boundary"], out)
			continue
		}
		out[mt] = decodeBody(t, p.Header.Get("Content-Transfer-Encoding"), p)
	}
}

func decodeBody(t *testing.T, encoding string, r io.Reader) string {
	t.Helper()
	raw, _ := io.ReadAll(r)
	if strings.EqualFold(strings.TrimSpace(encoding), "base64") {
		clean := strings.NewReplacer("\r", "", "\n", "").Replace(string(raw))
		dec, err := base64.StdEncoding.DecodeString(clean)
		if err != nil {
			t.Fatalf("base64 decode: %v", err)
		}
		return string(dec)
	}
	return string(raw)
}

func TestEncode_TextOnly(t *testing.T) {
	raw, err := encodeMessage(&Message{
		From: Address{Email: "f@x.com"}, To: []Address{{Email: "a@b.com"}},
		Subject: "Olá", Text: "corpo de texto",
	})
	if err != nil {
		t.Fatal(err)
	}
	msg, mt, parts := parseBack(t, raw)
	if mt != "text/plain" {
		t.Fatalf("expected text/plain, got %s", mt)
	}
	if parts["text/plain"] != "corpo de texto" {
		t.Fatalf("body mismatch: %q", parts["text/plain"])
	}
	if dec, _ := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject")); dec != "Olá" {
		t.Fatalf("subject not encoded/decoded: %q", dec)
	}
	if msg.Header.Get("Message-ID") == "" || msg.Header.Get("Date") == "" {
		t.Fatal("missing Message-ID/Date headers")
	}
}

func TestEncode_HTMLOnly(t *testing.T) {
	raw, _ := encodeMessage(&Message{
		From: Address{Email: "f@x.com"}, To: []Address{{Email: "a@b.com"}},
		Subject: "Hi", HTML: "<h1>Oi</h1>",
	})
	_, mt, parts := parseBack(t, raw)
	if mt != "text/html" || parts["text/html"] != "<h1>Oi</h1>" {
		t.Fatalf("html-only mismatch: mt=%s parts=%v", mt, parts)
	}
}

func TestEncode_Alternative(t *testing.T) {
	raw, _ := encodeMessage(&Message{
		From: Address{Email: "f@x.com"}, To: []Address{{Email: "a@b.com"}},
		Subject: "Hi", HTML: "<b>oi</b>", Text: "oi",
	})
	_, mt, parts := parseBack(t, raw)
	if mt != "multipart/alternative" {
		t.Fatalf("expected multipart/alternative, got %s", mt)
	}
	if parts["text/plain"] != "oi" || parts["text/html"] != "<b>oi</b>" {
		t.Fatalf("alternative parts mismatch: %v", parts)
	}
}

func TestEncode_Attachments(t *testing.T) {
	raw, _ := encodeMessage(&Message{
		From: Address{Email: "f@x.com"}, To: []Address{{Email: "a@b.com"}},
		Subject: "Hi", HTML: "<b>oi</b>", Text: "oi",
		Attachments: []Attachment{{Filename: "a.txt", ContentType: "text/plain", Content: []byte("anexo")}},
	})
	_, mt, parts := parseBack(t, raw)
	if mt != "multipart/mixed" {
		t.Fatalf("expected multipart/mixed, got %s", mt)
	}
	// nested alternative is flattened by collectParts; the attachment is text/plain too,
	// so we assert the html part and that the attachment content is present somewhere.
	if parts["text/html"] != "<b>oi</b>" {
		t.Fatalf("missing html part in mixed: %v", parts)
	}
	if !bytes.Contains(raw, []byte("Content-Disposition: attachment; filename=\"a.txt\"")) {
		t.Fatal("attachment disposition header missing")
	}
}

func TestEncode_CcAndHeaders(t *testing.T) {
	raw, _ := encodeMessage(&Message{
		From:    Address{Name: "BlueFamly", Email: "f@x.com"},
		To:      []Address{{Email: "a@b.com"}},
		Cc:      []Address{{Email: "c@b.com"}},
		Subject: "Hi", Text: "x",
		Headers: map[string]string{"X-Campaign": "signup"},
	})
	msg, _, _ := parseBack(t, raw)
	if msg.Header.Get("Cc") != "c@b.com" {
		t.Fatalf("Cc header mismatch: %q", msg.Header.Get("Cc"))
	}
	if msg.Header.Get("X-Campaign") != "signup" {
		t.Fatalf("custom header missing: %q", msg.Header.Get("X-Campaign"))
	}
	if !strings.Contains(msg.Header.Get("From"), "<f@x.com>") {
		t.Fatalf("From with name mismatch: %q", msg.Header.Get("From"))
	}
}
