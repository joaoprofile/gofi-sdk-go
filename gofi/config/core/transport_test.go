package core

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
)

func TestIsLoopback(t *testing.T) {
	for addr, want := range map[string]bool{
		"":                      true,
		"localhost":             true,
		"LOCALHOST:6379":        true,
		"127.0.0.1:5432":        true,
		"[::1]:6379":            true,
		"::1":                   true,
		"http://localhost:4317": true,
		"redis:6379":            false,
		"10.0.0.1:6379":         false,
		"https://otel.example":  false,
		"db.internal":           false,
	} {
		if got := IsLoopback(addr); got != want {
			t.Errorf("IsLoopback(%q)=%v, want %v", addr, got, want)
		}
	}
}

func TestInsecureTransports(t *testing.T) {
	if got := InsecureTransports(&environment.Environment{}); len(got) != 0 {
		t.Fatalf("default env: %+v", got)
	}
	got := InsecureTransports(&environment.Environment{
		TLSInsecureSkipVerify: true,
		BucketProvider:        "s3", BucketEndpoint: "http://minio:9000",
	})
	if len(got) != 2 || got[0].Resource != ResourceTLS || got[1].Resource != ResourceBucket {
		t.Fatalf("got %+v", got)
	}
}

func TestInsecureCache(t *testing.T) {
	for _, env := range []environment.Environment{
		{CacheURI: "localhost:6379"},
		{CacheURI: "redis:6379", CacheUseTLS: true},
	} {
		if got := InsecureCache(&env); got != nil {
			t.Errorf("%+v: %+v", env, got)
		}
	}
	got := InsecureCache(&environment.Environment{CacheURI: "redis://user:pw@redis:6379/0"})
	if len(got) != 1 || got[0].Resource != ResourceCache || got[0].Setting != "CACHE_USE_TLS" || strings.Contains(got[0].Detail, "pw") {
		t.Fatalf("got %+v", got)
	}
	if got := InsecureCache(&environment.Environment{CacheURI: "redis:6379"}); got[0].Detail != "redis redis:6379 without TLS" {
		t.Errorf("detail %q", got[0].Detail)
	}
}

func TestBucketOverHTTP(t *testing.T) {
	cases := []struct {
		env  environment.Environment
		want bool
	}{
		{environment.Environment{BucketProvider: "oci", BucketEndpoint: "http://x"}, false},
		{environment.Environment{BucketProvider: "s3"}, false},
		{environment.Environment{BucketProvider: "minio", BucketEndpoint: "localhost:9000"}, false},
		{environment.Environment{BucketProvider: "minio", BucketEndpoint: "http://minio:9000"}, true},
		{environment.Environment{BucketProvider: "s3", BucketEndpoint: "https://s3.example"}, false},
		{environment.Environment{BucketProvider: "s3", BucketEndpoint: "minio:9000"}, true},
		{environment.Environment{BucketProvider: "s3", BucketEndpoint: "minio:9000", BucketS3UseSSL: true}, false},
	}
	for _, c := range cases {
		if got := bucketOverHTTP(&c.env); got != c.want {
			t.Errorf("%s %s ssl=%v: got %v", c.env.BucketProvider, c.env.BucketEndpoint, c.env.BucketS3UseSSL, got)
		}
	}
}

var found = []InsecureTransport{
	{ResourceDatabase, "DATABASE_SSL_MODE", "sslmode disable"},
	{ResourceCache, "CACHE_USE_TLS", "plaintext"},
	{ResourceCache, "CACHE_USE_TLS", "plaintext"}, // duplicates are reported once
}

func TestCheckTransport_DevAndTestUnchecked(t *testing.T) {
	for _, e := range []string{"", "dev", "test"} {
		if err := CheckTransport(&environment.Environment{AppEnvironment: e}, found); err != nil {
			t.Errorf("%q: %v", e, err)
		}
	}
}

func TestCheckTransport_RefusedInProdAndStage(t *testing.T) {
	for _, e := range []string{"prod", "stage"} {
		err := CheckTransport(&environment.Environment{AppEnvironment: e}, found)
		if !errors.Is(err, ErrInsecureTransport) {
			t.Fatalf("%s: want ErrInsecureTransport, got %v", e, err)
		}
		msg := err.Error()
		if strings.Count(msg, "cache:") != 1 || !strings.Contains(msg, "DATABASE_SSL_MODE") ||
			!strings.Contains(msg, "GOFI_ALLOW_INSECURE_TRANSPORT=database") {
			t.Errorf("%s: message %q", e, msg)
		}
	}
}

// A plaintext HTTP server only warns in prod/stage, unless HTTP_REQUIRE_TLS.
func TestInsecureHTTP(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	logging.ResetForTesting() // logging.Warn falls back to slog.Default
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev); logging.ResetForTesting() })

	if got := InsecureHTTP(&environment.Environment{AppEnvironment: "prod", HTTPRequireTLS: true}, true); got != nil {
		t.Fatalf("TLS enabled: %+v", got)
	}
	for _, e := range []string{"", "dev", "test"} {
		if got := InsecureHTTP(&environment.Environment{AppEnvironment: e}, false); got != nil {
			t.Fatalf("%q: %+v", e, got)
		}
	}
	if strings.Contains(logs.String(), "without TLS") {
		t.Fatalf("warned outside prod/stage: %s", logs.String())
	}

	env := &environment.Environment{AppEnvironment: "prod"}
	if got := InsecureHTTP(env, false); got != nil {
		t.Fatalf("prod without HTTP_REQUIRE_TLS must only warn: %+v", got)
	}
	if !strings.Contains(logs.String(), "HTTP server without TLS") {
		t.Fatalf("no warning: %s", logs.String())
	}

	env.HTTPRequireTLS = true
	got := InsecureHTTP(env, false)
	if len(got) != 1 || got[0].Resource != ResourceHTTP {
		t.Fatalf("got %+v", got)
	}
	if err := CheckTransport(env, got); !errors.Is(err, ErrInsecureTransport) || !strings.Contains(err.Error(), "GOFI_ALLOW_INSECURE_TRANSPORT=http") {
		t.Fatalf("want refusal, got %v", err)
	}
}

func TestInsecureGRPC(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	logging.ResetForTesting() // logging.Warn falls back to slog.Default
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev); logging.ResetForTesting() })

	if got := InsecureGRPC(&environment.Environment{AppEnvironment: "prod", GRPCRequireTLS: true}, true); got != nil {
		t.Fatalf("TLS enabled: %+v", got)
	}
	if got := InsecureGRPC(&environment.Environment{AppEnvironment: "dev"}, false); got != nil || logs.Len() > 0 {
		t.Fatalf("dev: %+v %s", got, logs.String())
	}

	env := &environment.Environment{AppEnvironment: "stage"}
	if got := InsecureGRPC(env, false); got != nil {
		t.Fatalf("stage without GRPC_REQUIRE_TLS must only warn: %+v", got)
	}
	if !strings.Contains(logs.String(), "gRPC server without TLS") {
		t.Fatalf("no warning: %s", logs.String())
	}

	env.GRPCRequireTLS = true
	got := InsecureGRPC(env, false)
	if len(got) != 1 || got[0].Resource != ResourceGRPC {
		t.Fatalf("got %+v", got)
	}
	if err := CheckTransport(env, got); !errors.Is(err, ErrInsecureTransport) || !strings.Contains(err.Error(), "GOFI_ALLOW_INSECURE_TRANSPORT=grpc") {
		t.Fatalf("want refusal, got %v", err)
	}
}

func TestCheckTransport_Allowed(t *testing.T) {
	logging.NewLogger("test")
	env := &environment.Environment{AppEnvironment: "prod", AllowInsecureTransport: " Database , ,cache"}
	if err := CheckTransport(env, found); err != nil {
		t.Fatalf("allowed resources refused: %v", err)
	}
	env.AllowInsecureTransport = "all"
	if err := CheckTransport(env, found); err != nil {
		t.Fatalf("all refused: %v", err)
	}
	env.AllowInsecureTransport = "cache"
	if err := CheckTransport(env, found); err == nil || strings.Contains(err.Error(), "cache:") {
		t.Fatalf("only database must be refused: %v", err)
	}
}
