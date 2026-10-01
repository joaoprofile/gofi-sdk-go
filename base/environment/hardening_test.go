package environment

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_InvalidAppEnvironmentFailsFast(t *testing.T) {
	reset(t)
	t.Setenv("GOFI_DOTENV", "false")
	t.Setenv("APP_ENVIRONMENT", "production")
	_, err := Load()
	if !errors.Is(err, ErrInvalidEnvironment) || !strings.Contains(err.Error(), "APP_ENVIRONMENT") {
		t.Fatalf("err=%v, want ErrInvalidEnvironment naming APP_ENVIRONMENT", err)
	}
	if LoadError() == nil {
		t.Fatal("Instance load must report the invalid APP_ENVIRONMENT")
	}
}

func TestLoad_ParseErrorDoesNotEchoValue(t *testing.T) {
	t.Setenv("GOFI_DOTENV", "false")
	t.Setenv("DATABASE_MIGRATION", "s3cr3t-pa55")
	_, err := Load()
	if err == nil || strings.Contains(err.Error(), "s3cr3t") || !strings.Contains(err.Error(), "DATABASE_MIGRATION") {
		t.Fatalf("err=%v, want the variable name without its value", err)
	}
}

// chdir switches to dir for the rest of the test.
func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFindProjectRoot_StopsAtModuleRoot(t *testing.T) {
	top := t.TempDir()
	touch(t, filepath.Join(top, ".env"))           // above the module: must be ignored
	touch(t, filepath.Join(top, "mod", "go.mod"))  // module root
	sub := filepath.Join(top, "mod", "cmd", "api") // working directory
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	chdir(t, sub)
	if root, err := findProjectRoot(); err != nil || root != "" {
		t.Fatalf("root=%q err=%v, want no .env (parent of module root)", root, err)
	}

	touch(t, filepath.Join(top, "mod", ".env"))
	if root, _ := findProjectRoot(); root != filepath.Join(top, "mod") {
		t.Fatalf("root=%q, want the module root", root)
	}
}

func TestFindProjectRoot_OutsideModuleOnlyUsesCWD(t *testing.T) {
	top := t.TempDir()
	if moduleRoot(top) != "" {
		t.Skip("temp dir is inside a Go module")
	}
	touch(t, filepath.Join(top, ".env"))
	sub := filepath.Join(top, "app")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	chdir(t, sub)
	if root, _ := findProjectRoot(); root != "" {
		t.Fatalf("root=%q, want only the working directory to be searched", root)
	}
	chdir(t, top)
	if root, _ := findProjectRoot(); root != top {
		t.Fatalf("root=%q, want %q", root, top)
	}
}

// assertNoSecret checks fmt, slog (JSON and text) and encoding/json output.
func assertNoSecret(t *testing.T, v any, secrets ...string) {
	t.Helper()
	var outs []string
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%d", "%q", "%x"} {
		outs = append(outs, fmt.Sprintf(verb, v))
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("cfg", "cfg", v)
	slog.New(slog.NewTextHandler(&buf, nil)).Info("cfg", "cfg", v)
	outs = append(outs, buf.String())
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	outs = append(outs, string(b))
	for _, out := range outs {
		for _, s := range secrets {
			if strings.Contains(out, s) || strings.Contains(out, fmt.Sprintf("%x", s)) {
				t.Fatalf("secret %q leaked in %s", s, out)
			}
		}
	}
	if !strings.Contains(buf.String(), "[REDACTED]") {
		t.Errorf("expected the mask in the log output: %s", buf.String())
	}
}

func TestEnvironment_SecretsAreRedacted(t *testing.T) {
	env := Environment{
		AppName:                 "svc",
		ServiceDebugPass:        "sec-debug",
		DatabasePassword:        "sec-db",
		CacheURI:                "redis://:sec-uri@cache:6379/0",
		CachePassword:           "sec-cache",
		MessagingPassword:       "sec-mq",
		MessagingOCIPrivateKey:  "sec-mq-oci",
		OCIPrivateKey:           "sec-oci",
		BucketOCIPrivateKey:     "sec-bucket-oci",
		BucketOCIPassphrase:     "sec-pass",
		BucketS3SecretKey:       "sec-s3",
		OtelExporterOTLPHeaders: "authorization=Bearer sec-otlp",
		JWTSecret:               "sec-jwt",
		JWTPreviousSecret:       "sec-jwt-prev",
		OAuthGoogleClientSecret: "sec-google",
		MailPassword:            "sec-mail",
	}
	assertNoSecret(t, env, "sec-")
	assertNoSecret(t, &env, "sec-")
	assertNoSecret(t, env.Auth(), "sec-")
	assertNoSecret(t, env.OAuth(), "sec-")
	assertNoSecret(t, env.Observability(), "sec-")

	if s := fmt.Sprintf("%+v", env); !strings.Contains(s, "AppName:svc") || !strings.Contains(s, "cache:6379") ||
		!strings.Contains(s, "MailHost: ") {
		t.Errorf("non-secret fields must stay readable: %s", s)
	}
	var m map[string]any
	b, _ := json.Marshal(Environment{JWTSecret: "x"})
	if err := json.Unmarshal(b, &m); err != nil || m["JWTSecret"] != "[REDACTED]" || m["DatabasePassword"] != "" {
		t.Errorf("json: %s (%v)", b, err)
	}
}
