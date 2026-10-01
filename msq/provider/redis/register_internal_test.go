package redis

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/msq"
	goredis "github.com/redis/go-redis/v9"
)

func TestConfigFrom(t *testing.T) {
	cfg, err := configFrom(msq.ProviderConfig{Redis: msq.RedisConnection{Addr: "localhost:6379", Password: "pw", UseTLS: true, Mode: "streams"}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "localhost:6379" || cfg.Password != "pw" || !cfg.TLSEnabled || cfg.Mode != "streams" {
		t.Errorf("not mapped: %+v", cfg)
	}
}

func TestConfigFromTLSBlock(t *testing.T) {
	cfg, err := configFrom(msq.ProviderConfig{Redis: msq.RedisConnection{Addr: "cache:6380"}, TLS: msq.TLSConfig{ServerName: "cache.internal"}})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.TLSEnabled || cfg.TLS == nil || cfg.TLS.ServerName != "cache.internal" {
		t.Errorf("TLS block not mapped: %+v", cfg.TLS)
	}
	got := New(cfg).client.(*goredis.Client).Options().TLSConfig
	if got == nil || got.ServerName != "cache.internal" || got.MinVersion < tls.VersionTLS12 {
		t.Errorf("client TLS = %+v", got)
	}
	if _, err := configFrom(msq.ProviderConfig{TLS: msq.TLSConfig{CertFile: "c.pem"}}); err == nil {
		t.Error("an invalid TLS block must fail")
	}
}

func TestConfigRedactsPassword(t *testing.T) {
	cfg := Config{Addr: "cache:6379", Password: "s3cret"}
	js, _ := json.Marshal(cfg)
	for _, out := range []string{fmt.Sprintf("%v", cfg), fmt.Sprintf("%+v", cfg), fmt.Sprintf("%#v", cfg), string(js), cfg.LogValue().String()} {
		if strings.Contains(out, "s3cret") || !strings.Contains(out, "REDACTED") {
			t.Errorf("password not redacted: %s", out)
		}
	}
}
