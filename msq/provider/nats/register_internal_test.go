package nats

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/msq"
)

func TestConfigFrom(t *testing.T) {
	cfg, err := configFrom(msq.ProviderConfig{ClientName: "billing", Addr: "nats:4222", User: "u", Password: "p", UseTLS: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.URL != "tls://nats:4222" || cfg.Name != "billing" || cfg.User != "u" || cfg.Password != "p" || cfg.TLS == nil {
		t.Errorf("not mapped: %+v", cfg)
	}
}

func TestConfigFromTLSBlock(t *testing.T) {
	cfg, err := configFrom(msq.ProviderConfig{Addr: "nats:4222", TLS: msq.TLSConfig{ServerName: "nats.internal"}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.URL != "tls://nats:4222" || cfg.TLS.ServerName != "nats.internal" {
		t.Errorf("TLS block not mapped: %v %+v", cfg.URL, cfg.TLS)
	}
	cfg, _ = configFrom(msq.ProviderConfig{Addr: "nats:4222"})
	if cfg.URL != "nats://nats:4222" || cfg.TLS != nil {
		t.Errorf("plaintext must stay plaintext: %+v", cfg)
	}
	if _, err := configFrom(msq.ProviderConfig{TLS: msq.TLSConfig{KeyFile: "k.pem"}}); err == nil {
		t.Error("an invalid TLS block must fail")
	}
}

func TestConfigRedactsSecrets(t *testing.T) {
	cfg := Config{URL: "nats://n:4222", Token: "s3cret-token", User: "u", Password: "s3cret-pw"}
	js, _ := json.Marshal(cfg)
	for _, out := range []string{fmt.Sprintf("%v", cfg), fmt.Sprintf("%+v", cfg), fmt.Sprintf("%#v", cfg), string(js), cfg.LogValue().String()} {
		if strings.Contains(out, "s3cret") || !strings.Contains(out, "REDACTED") {
			t.Errorf("secret not redacted: %s", out)
		}
	}
}
