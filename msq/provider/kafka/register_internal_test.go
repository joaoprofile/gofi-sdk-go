package kafka

import (
	"testing"

	"github.com/gofi-labs/gofi-sdk-go/msq"
)

func TestConfigFrom(t *testing.T) {
	cfg, err := configFrom(msq.ProviderConfig{Addr: "broker:9092", User: "u", Password: "p", UseTLS: true, SASLMechanism: "SCRAM-SHA-256"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Brokers) != 1 || cfg.Brokers[0] != "broker:9092" {
		t.Errorf("brokers not mapped: %+v", cfg.Brokers)
	}
	if cfg.User != "u" || cfg.Password != "p" || !cfg.UseTLS || cfg.SASLMechanism != "SCRAM-SHA-256" || cfg.TLS == nil {
		t.Errorf("creds/tls not mapped: %+v", cfg)
	}
}

func TestConfigFromTLSBlock(t *testing.T) {
	cfg, err := configFrom(msq.ProviderConfig{Addr: "b:9093", TLS: msq.TLSConfig{ServerName: "kafka.internal"}})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.UseTLS || cfg.TLS == nil || cfg.TLS.ServerName != "kafka.internal" {
		t.Errorf("TLS block not mapped: %+v", cfg.TLS)
	}
	if _, err := configFrom(msq.ProviderConfig{TLS: msq.TLSConfig{CertFile: "only-cert.pem"}}); err == nil {
		t.Error("an invalid TLS block must fail")
	}
}

func TestConfigFromAllowPlaintextSASL(t *testing.T) {
	cfg, err := configFrom(msq.ProviderConfig{Addr: "b:9092", User: "u", Password: "p", AllowPlaintextSASL: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(cfg); err != nil {
		t.Errorf("explicit opt-in must allow PLAIN without TLS: %v", err)
	}
}
