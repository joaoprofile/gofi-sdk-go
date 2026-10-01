package observability

import (
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/base/environment"
)

func TestObservability_MapsEnv(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	t.Setenv("APP_NAME", "billing")
	t.Setenv("APP_VERSION", "1.4.0")
	t.Setenv("APP_ENVIRONMENT", "prod")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "otel:4317")

	cfg := ConfigFromEnv(environment.Instance())
	if cfg.ServiceName != "billing" || cfg.ServiceVersion != "1.4.0" || cfg.ServiceEnv != "prod" || cfg.CollectorAddr != "otel:4317" {
		t.Errorf("observability not mapped: %+v", cfg)
	}
	if !cfg.TLS {
		t.Error("OTLP must use TLS by default")
	}
}

func TestObservability_MapsTLSFiles(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://otel:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_CERTIFICATE", " /tls/ca.pem ")
	t.Setenv("OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE", "/tls/client.pem")
	t.Setenv("OTEL_EXPORTER_OTLP_CLIENT_KEY", "/tls/client-key.pem")

	cfg := ConfigFromEnv(environment.Instance())
	if !cfg.TLS || cfg.CAFile != "/tls/ca.pem" || cfg.CertFile != "/tls/client.pem" || cfg.KeyFile != "/tls/client-key.pem" {
		t.Errorf("OTLP TLS files not mapped: %+v", cfg)
	}
	if err := validateTLS(cfg); err != nil {
		t.Errorf("valid TLS files rejected: %v", err)
	}
}

func TestValidateTLS(t *testing.T) {
	cases := []struct {
		name string
		env  environment.Environment
		ok   bool
	}{
		{"plaintext without files", environment.Environment{OtelExporterOTLPEndpoint: "http://otel:4317"}, true},
		{"TLS with CA only", environment.Environment{OtelExporterOTLPEndpoint: "otel:4317", OtelExporterOTLPCertificate: "ca"}, true},
		{"CA with http endpoint", environment.Environment{OtelExporterOTLPEndpoint: "http://otel:4317", OtelExporterOTLPCertificate: "ca"}, false},
		{"client cert with insecure", environment.Environment{
			OtelExporterOTLPEndpoint: "otel:4317", OtelExporterOTLPInsecure: "true",
			OtelExporterOTLPClientCertificate: "c", OtelExporterOTLPClientKey: "k",
		}, false},
		{"key with http endpoint", environment.Environment{OtelExporterOTLPEndpoint: "http://otel:4317", OtelExporterOTLPClientKey: "k"}, false},
		{"cert without key", environment.Environment{OtelExporterOTLPEndpoint: "otel:4317", OtelExporterOTLPClientCertificate: "c"}, false},
		{"key without cert", environment.Environment{OtelExporterOTLPEndpoint: "otel:4317", OtelExporterOTLPClientKey: "k"}, false},
	}
	for _, c := range cases {
		err := validateTLS(ConfigFromEnv(&c.env))
		if (err == nil) != c.ok {
			t.Errorf("%s: err=%v, want ok=%v", c.name, err, c.ok)
		}
	}
}

func TestOTLPEndpoint(t *testing.T) {
	cases := []struct {
		endpoint, insecure string
		addr               string
		tls                bool
	}{
		{"otel:4317", "", "otel:4317", true},
		{"otel:4317", "false", "otel:4317", true},
		{"otel:4317", "TRUE", "otel:4317", false},
		{"http://otel:4317", "", "otel:4317", false},
		{"https://otel:4317/", "true", "otel:4317", true}, // the scheme wins
		{"HTTP://otel:4317/v1", "", "otel:4317", false},
		{"dns:///otel:4317", "", "dns:///otel:4317", true},
		{"unix:///run/otel.sock", "true", "unix:///run/otel.sock", false},
	}
	for _, c := range cases {
		cfg := ConfigFromEnv(&environment.Environment{OtelExporterOTLPEndpoint: c.endpoint, OtelExporterOTLPInsecure: c.insecure})
		if cfg.CollectorAddr != c.addr || cfg.TLS != c.tls {
			t.Errorf("%q insecure=%q: addr=%q tls=%v, want %q %v", c.endpoint, c.insecure, cfg.CollectorAddr, cfg.TLS, c.addr, c.tls)
		}
	}
}
