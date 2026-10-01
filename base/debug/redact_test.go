package debug

import (
	"bytes"

	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestConfig_SecretsAreRedacted(t *testing.T) {
	c := Config{Addr: ":6060", User: "admin", Pass: "sec-pass"}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("m", "cfg", c)
	slog.New(slog.NewTextHandler(&buf, nil)).Info("m", "cfg", &c)
	js, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	outs := []string{buf.String(), string(js)}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%d"} {
		outs = append(outs, fmt.Sprintf(verb, c), fmt.Sprintf(verb, &c))
	}
	for _, o := range outs {
		if strings.Contains(o, "sec-") || !strings.Contains(o, "[REDACTED]") {
			t.Errorf("secret not redacted: %s", o)
		}
	}
}

func TestServer_DoesNotPrintPassword(t *testing.T) {
	s := New(Config{User: "admin", Pass: "sec-pass"})
	for _, verb := range []string{"%v", "%+v", "%#v", "%d"} {
		if out := fmt.Sprintf(verb, s); strings.Contains(out, "sec-") {
			t.Errorf("%s leaked: %s", verb, out)
		}
	}
}
