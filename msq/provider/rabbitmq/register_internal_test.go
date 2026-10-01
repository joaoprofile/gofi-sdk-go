package rabbitmq

import (
	"context"
	"crypto/tls"
	"strings"
	"testing"

	"github.com/rabbitmq/amqp091-go"

	"github.com/gofi-labs/gofi-sdk-go/msq"
	"github.com/gofi-labs/gofi-sdk-go/msq/types"
)

func TestDialConfig(t *testing.T) {
	cfg := msq.ProviderConfig{User: "guest", Password: "guest", Addr: "localhost:5672"}
	got, opts, err := dialConfig(cfg)
	if err != nil || len(opts) != 0 {
		t.Fatal(err, opts)
	}
	if want := "amqp://guest:guest@localhost:5672/"; got != want {
		t.Errorf("url=%q, want %q", got, want)
	}
}

func TestDialConfig_EscapesCredentialsAndTLS(t *testing.T) {
	cfg := msq.ProviderConfig{User: "svc", Password: "p@ss/w:rd", Addr: "mq:5671", UseTLS: true, TLS: msq.TLSConfig{ServerName: "mq.internal"}}
	got, opts, err := dialConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if want := "amqps://svc:p%40ss%2Fw%3Ard@mq:5671/"; got != want {
		t.Errorf("url=%q, want %q", got, want)
	}
	_, dc, err := dialTarget(got, opts)
	if err != nil || dc.TLSClientConfig == nil || dc.TLSClientConfig.ServerName != "mq.internal" {
		t.Errorf("TLS block not mapped: %v %+v", err, dc.TLSClientConfig)
	}
}

// The password must never reach an error, whatever is wrong with the address.
func TestDialConfig_InvalidAddressDoesNotLeakPassword(t *testing.T) {
	for _, addr := range []string{"bad host:5672", "bad%20host:5672", "host", ":5672", "a@b:5672", "h/x:1"} {
		_, _, err := dialConfig(msq.ProviderConfig{User: "u", Password: "s3cret", Addr: addr})
		if err == nil {
			t.Errorf("%q: invalid address accepted", addr)
			continue
		}
		if strings.Contains(err.Error(), "s3cret") {
			t.Errorf("%q: error leaks the password: %v", addr, err)
		}
	}
}

func TestOpen_ErrorsDoNotLeakPassword(t *testing.T) {
	for _, addr := range []string{"bad%20host:5672", "127.0.0.1:1"} {
		_, err := msq.Open(context.Background(), msq.ProviderConfig{Type: msq.BrokerRabbitMQ, User: "u", Password: "s3cret", Addr: addr})
		if err == nil {
			t.Fatalf("%q: dial must fail", addr)
		}
		if strings.Contains(err.Error(), "s3cret") {
			t.Errorf("%q: error leaks the password: %v", addr, err)
		}
	}
}

func TestDialURL_ErrorsDoNotLeakPassword(t *testing.T) {
	for _, raw := range []string{"amqp://u:s3cret@bad%20host:5672/", "amqp://u:s3cret@127.0.0.1:1/", "http://u:s3cret@h:1/", "amqp://u:s3cret@/"} {
		_, err := DialURL(raw)
		if err == nil {
			t.Fatalf("%q: dial must fail", raw)
		}
		if strings.Contains(err.Error(), "s3cret") {
			t.Errorf("error leaks the password: %v", err)
		}
	}
}

func TestDialTarget_MovesCredentialsToSASL(t *testing.T) {
	target, cfg, err := dialTarget("amqp://svc:p%40ss@mq:5672/vh", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(target, "svc") || strings.Contains(target, "p@ss") {
		t.Errorf("target keeps credentials: %q", target)
	}
	plain, ok := cfg.SASL[0].(*amqp091.PlainAuth)
	if !ok || plain.Username != "svc" || plain.Password != "p@ss" {
		t.Errorf("SASL = %+v", cfg.SASL)
	}
	if _, _, err := dialTarget("amqp://mq:5672/", []DialOption{WithTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12})}); err == nil {
		t.Error("TLS over amqp:// must fail")
	}
}

func TestEncoding(t *testing.T) {
	for in, want := range map[types.Encoding]types.Encoding{"": types.EncodingEnvelope, "envelope": types.EncodingEnvelope, "cloudevents": types.EncodingCloudEvents} {
		got, err := encoding(in)
		if err != nil || got != want {
			t.Errorf("encoding(%q)=%q,%v want %q", in, got, err, want)
		}
	}
	if _, err := encoding("avro"); err == nil {
		t.Error("unknown encoding must fail")
	}
}
