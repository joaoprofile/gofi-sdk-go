package redact_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/base/redact"
)

type inner struct {
	Key string `redact:"true"`
}

func (c inner) Format(f fmt.State, verb rune) { redact.Format(f, verb, c) }
func (c inner) LogValue() slog.Value          { return redact.LogValue(c) }
func (c inner) MarshalJSON() ([]byte, error)  { return redact.JSON(c) }

type cfg struct {
	User     string
	Password string `redact:"true"`
	Token    []byte `redact:"true"`
	Empty    string `redact:"true"`
	URI      string `redact:"url"`
	Port     int    `json:"port,omitempty"`
	Skip     string `json:"-"`
	Nested   inner
	hidden   string
}

func (c cfg) String() string                { return redact.Sprint(c) }
func (c cfg) GoString() string              { return redact.GoSprint(c) }
func (c cfg) Format(f fmt.State, verb rune) { redact.Format(f, verb, c) }
func (c cfg) LogValue() slog.Value          { return redact.LogValue(c) }
func (c cfg) MarshalJSON() ([]byte, error)  { return redact.JSON(c) }

func sample() cfg {
	return cfg{
		User: "app", Password: "sec-pw", Token: []byte("sec-token"),
		URI: "postgres://app:sec-uri@db:5432/x", Skip: "skip", Nested: inner{Key: "sec-nested"}, hidden: "sec-hidden",
	}
}

func TestRedact_AllRenderings(t *testing.T) {
	c := sample()
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("m", "cfg", c)
	slog.New(slog.NewTextHandler(&buf, nil)).Info("m", "cfg", &c)
	js, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	outs := []string{c.String(), c.GoString(), buf.String(), string(js)}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%d", "%q"} {
		outs = append(outs, fmt.Sprintf(verb, c), fmt.Sprintf(verb, &c))
	}
	for _, o := range outs {
		if strings.Contains(o, "sec-") {
			t.Errorf("secret leaked: %s", o)
		}
	}
}

func TestRedact_Shapes(t *testing.T) {
	c := sample()
	if got, want := fmt.Sprintf("%+v", c), "{User:app Password:[REDACTED] Token:[REDACTED] Empty: URI:postgres://app:[REDACTED]@db:5432/x Port:0 Skip:skip Nested:{Key:[REDACTED]}}"; got != want {
		t.Errorf("%%+v\n got %s\nwant %s", got, want)
	}
	if got := fmt.Sprintf("%#v", c); !strings.HasPrefix(got, `redact_test.cfg{User:"app", Password:"[REDACTED]"`) {
		t.Errorf("%%#v = %s", got)
	}
	js, _ := json.Marshal(c)
	if got, want := string(js), `{"User":"app","Password":"[REDACTED]","Token":"[REDACTED]","Empty":"","URI":"postgres://app:[REDACTED]@db:5432/x","Nested":{"Key":"[REDACTED]"}}`; got != want {
		t.Errorf("json\n got %s\nwant %s", got, want)
	}
	var nilCfg *cfg
	if b, err := json.Marshal(nilCfg); err != nil || string(b) != "null" {
		t.Errorf("nil pointer json = %s, %v", b, err)
	}
}

func TestURL(t *testing.T) {
	for in, want := range map[string]string{
		"":                          "",
		"redis://cache:6379":        "redis://cache:6379",
		"redis://:pw@cache:6379/0":  "redis://:[REDACTED]@cache:6379/0",
		"amqp://u:p@h/vhost":        "amqp://u:[REDACTED]@h/vhost",
		"://bad url with pw\x7f":    redact.Mask,
		"postgres://only-user@db/x": "postgres://only-user@db/x",
	} {
		if got := redact.URL(in); got != want {
			t.Errorf("URL(%q)=%q, want %q", in, got, want)
		}
	}
	if redact.String("") != "" || redact.String("x") != redact.Mask {
		t.Error("String")
	}
}
