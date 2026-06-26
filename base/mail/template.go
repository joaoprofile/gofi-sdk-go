package mail

import (
	"bytes"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"strings"
	"sync"
	texttemplate "text/template"
)

// ErrTemplateNotFound is returned when rendering an unregistered template.
var ErrTemplateNotFound = errors.New("mail: template not found")

// TemplateSource holds the raw template strings for one named message. Subject
// and Text use text/template; HTML uses html/template (auto-escaping). Any field
// may be empty (e.g. HTML-only or Text-only messages).
type TemplateSource struct {
	Subject string
	HTML    string
	Text    string
}

type compiledTemplate struct {
	subject *texttemplate.Template
	html    *htmltemplate.Template
	text    *texttemplate.Template
}

// TemplateEngine compiles and renders named e-mail templates. Safe for
// concurrent use.
type TemplateEngine struct {
	mu    sync.RWMutex
	items map[string]*compiledTemplate
}

// NewTemplateEngine returns an empty engine.
func NewTemplateEngine() *TemplateEngine {
	return &TemplateEngine{items: make(map[string]*compiledTemplate)}
}

// Register compiles a template under name. Re-registering replaces it. Returns a
// parse error if any source is malformed.
func (e *TemplateEngine) Register(name string, src TemplateSource) error {
	ct := &compiledTemplate{}
	if strings.TrimSpace(src.Subject) != "" {
		t, err := texttemplate.New(name + ":subject").Parse(src.Subject)
		if err != nil {
			return fmt.Errorf("mail: parse subject template %q: %w", name, err)
		}
		ct.subject = t
	}
	if strings.TrimSpace(src.HTML) != "" {
		t, err := htmltemplate.New(name + ":html").Parse(src.HTML)
		if err != nil {
			return fmt.Errorf("mail: parse html template %q: %w", name, err)
		}
		ct.html = t
	}
	if strings.TrimSpace(src.Text) != "" {
		t, err := texttemplate.New(name + ":text").Parse(src.Text)
		if err != nil {
			return fmt.Errorf("mail: parse text template %q: %w", name, err)
		}
		ct.text = t
	}
	e.mu.Lock()
	e.items[name] = ct
	e.mu.Unlock()
	return nil
}

// Has reports whether a template is registered.
func (e *TemplateEngine) Has(name string) bool {
	e.mu.RLock()
	_, ok := e.items[name]
	e.mu.RUnlock()
	return ok
}

// Render executes the named template with data, returning subject, html and text.
func (e *TemplateEngine) Render(name string, data any) (subject, html, text string, err error) {
	e.mu.RLock()
	ct, ok := e.items[name]
	e.mu.RUnlock()
	if !ok {
		return "", "", "", fmt.Errorf("%w: %s", ErrTemplateNotFound, name)
	}
	var buf bytes.Buffer
	if ct.subject != nil {
		buf.Reset()
		if err = ct.subject.Execute(&buf, data); err != nil {
			return "", "", "", fmt.Errorf("mail: render subject %q: %w", name, err)
		}
		subject = strings.TrimSpace(buf.String())
	}
	if ct.html != nil {
		buf.Reset()
		if err = ct.html.Execute(&buf, data); err != nil {
			return "", "", "", fmt.Errorf("mail: render html %q: %w", name, err)
		}
		html = buf.String()
	}
	if ct.text != nil {
		buf.Reset()
		if err = ct.text.Execute(&buf, data); err != nil {
			return "", "", "", fmt.Errorf("mail: render text %q: %w", name, err)
		}
		text = buf.String()
	}
	return subject, html, text, nil
}

// RenderMessage renders the named template into a ready-to-send Message addressed
// from `from` to `to`.
func (e *TemplateEngine) RenderMessage(name string, from Address, to []Address, data any) (*Message, error) {
	subject, html, text, err := e.Render(name, data)
	if err != nil {
		return nil, err
	}
	return &Message{From: from, To: to, Subject: subject, HTML: html, Text: text}, nil
}
