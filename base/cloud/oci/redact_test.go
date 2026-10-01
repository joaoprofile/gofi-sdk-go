package oci

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestConfig_SecretsAreRedacted(t *testing.T) {
	c := Config{Region: "sa-saopaulo-1", TenancyID: "ocid1.tenancy", PrivateKey: "sec-pem", Passphrase: "sec-pass"}
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
	if got := fmt.Sprintf("%#v", c); !strings.HasPrefix(got, "oci.Config{AuthMode:\"\", Region:\"sa-saopaulo-1\"") {
		t.Errorf("%%#v = %s", got)
	}
	var empty Config
	if got := fmt.Sprintf("%+v", empty); strings.Contains(got, "[REDACTED]") {
		t.Errorf("empty secrets must render empty: %s", got)
	}
}
