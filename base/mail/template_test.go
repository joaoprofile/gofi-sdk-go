package mail

import (
	"errors"
	"strings"
	"testing"
)

func TestTemplate_RegisterAndRender(t *testing.T) {
	e := NewTemplateEngine()
	if err := e.Register("welcome", TemplateSource{
		Subject: "Olá {{.Name}}",
		HTML:    "<h1>Bem-vindo, {{.Name}}</h1>",
		Text:    "Bem-vindo, {{.Name}}",
	}); err != nil {
		t.Fatal(err)
	}
	if !e.Has("welcome") {
		t.Fatal("Has should report registered template")
	}
	subject, html, text, err := e.Render("welcome", map[string]string{"Name": "Ana"})
	if err != nil {
		t.Fatal(err)
	}
	if subject != "Olá Ana" || html != "<h1>Bem-vindo, Ana</h1>" || text != "Bem-vindo, Ana" {
		t.Fatalf("render mismatch: %q / %q / %q", subject, html, text)
	}
}

func TestTemplate_HTMLAutoEscape(t *testing.T) {
	e := NewTemplateEngine()
	_ = e.Register("x", TemplateSource{HTML: "<p>{{.V}}</p>"})
	_, html, _, err := e.Render("x", map[string]string{"V": "<script>"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "<script>") {
		t.Fatalf("html/template must escape, got %q", html)
	}
}

func TestTemplate_NotFound(t *testing.T) {
	e := NewTemplateEngine()
	if _, _, _, err := e.Render("missing", nil); !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("expected ErrTemplateNotFound, got %v", err)
	}
}

func TestTemplate_ParseError(t *testing.T) {
	e := NewTemplateEngine()
	if err := e.Register("bad", TemplateSource{Subject: "{{.Name"}); err == nil {
		t.Fatal("expected parse error for malformed template")
	}
}

func TestTemplate_RenderMessage(t *testing.T) {
	e := NewTemplateEngine()
	_ = e.Register("welcome", TemplateSource{Subject: "Oi {{.Name}}", HTML: "<b>{{.Name}}</b>"})
	msg, err := e.RenderMessage("welcome",
		Address{Email: "f@x.com"}, []Address{{Email: "a@b.com"}}, map[string]string{"Name": "Ana"})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "Oi Ana" || msg.HTML != "<b>Ana</b>" || msg.To[0].Email != "a@b.com" {
		t.Fatalf("RenderMessage mismatch: %+v", msg)
	}
	if err := msg.validate(); err != nil {
		t.Fatalf("rendered message should be valid: %v", err)
	}
}
