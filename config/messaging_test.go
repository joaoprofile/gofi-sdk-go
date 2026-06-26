package config

import (
	"testing"

	"github.com/joaoprofile/gofi/base/environment"
)

func TestKafka_MapsEnv(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	t.Setenv("MESSAGING_HOST", "broker")
	t.Setenv("MESSAGING_PORT", "9092")
	t.Setenv("MESSAGING_USER", "u")
	t.Setenv("MESSAGING_PASSWORD", "p")
	t.Setenv("MESSAGING_USE_TLS", "true")
	t.Setenv("MESSAGING_SASL_MECHANISM", "SCRAM-SHA-256")

	cfg := Kafka(environment.Instance())
	if len(cfg.Brokers) != 1 || cfg.Brokers[0] != "broker:9092" {
		t.Errorf("brokers not mapped: %+v", cfg.Brokers)
	}
	if cfg.User != "u" || cfg.Password != "p" || !cfg.UseTLS {
		t.Errorf("creds/tls not mapped: %+v", cfg)
	}
	if cfg.SASLMechanism != "SCRAM-SHA-256" {
		t.Errorf("sasl not mapped: %q", cfg.SASLMechanism)
	}
}

func TestOCIQueue_MapsEnv(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	t.Setenv("MESSAGING_OCI_TENANCY_ID", "ten")
	t.Setenv("MESSAGING_OCI_USER_ID", "usr")
	t.Setenv("MESSAGING_OCI_REGION", "sa-saopaulo-1")
	t.Setenv("MESSAGING_OCI_FINGERPRINT", "aa:bb")

	cfg := OCIQueue(environment.Instance())
	if cfg.TenancyID != "ten" || cfg.UserID != "usr" || cfg.Region != "sa-saopaulo-1" || cfg.FingerPrint != "aa:bb" {
		t.Errorf("oci queue creds not mapped: %+v", cfg)
	}
}

func TestRabbitMQURL_MapsEnv(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	t.Setenv("MESSAGING_USER", "guest")
	t.Setenv("MESSAGING_PASSWORD", "guest")
	t.Setenv("MESSAGING_HOST", "localhost")
	t.Setenv("MESSAGING_PORT", "5672")

	got := RabbitMQURL(environment.Instance())
	want := "amqp://guest:guest@localhost:5672/"
	if got != want {
		t.Errorf("RabbitMQURL=%q, want %q", got, want)
	}
}
