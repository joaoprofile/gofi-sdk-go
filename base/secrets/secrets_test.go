package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestParseRef(t *testing.T) {
	ref, err := ParseRef("secret://awssm/prod/db#password")
	if err != nil || ref != (Ref{Provider: "awssm", Name: "prod/db", Key: "password"}) {
		t.Errorf("ParseRef=%+v,%v", ref, err)
	}
	for _, bad := range []string{"awssm/x", "secret://", "secret://awssm", "secret:///x"} {
		if _, err := ParseRef(bad); !errors.Is(err, ErrInvalidRef) {
			t.Errorf("ParseRef(%q) err=%v", bad, err)
		}
	}
}

func TestResolve_PlainValuePassesThrough(t *testing.T) {
	if v, err := Resolve(context.Background(), "plain"); v != "plain" || err != nil {
		t.Errorf("got %q,%v", v, err)
	}
}

func TestResolve_EnvAndFile(t *testing.T) {
	t.Setenv("GOFI_TEST_SECRET", "s3cr3t")
	if v, err := Resolve(context.Background(), "secret://env/GOFI_TEST_SECRET"); v != "s3cr3t" || err != nil {
		t.Errorf("env: %q,%v", v, err)
	}
	path := filepath.Join(t.TempDir(), "db")
	if err := os.WriteFile(path, []byte(`{"user":"app","password":"pw","port":5432}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewResolver()
	ref := "secret://file" + path
	if v, err := r.Resolve(context.Background(), ref+"#password"); v != "pw" || err != nil {
		t.Errorf("file key: %q,%v", v, err)
	}
	if v, _ := r.Resolve(context.Background(), ref+"#port"); v != "5432" {
		t.Errorf("non-string key: %q", v)
	}
	if _, err := r.Resolve(context.Background(), ref+"#missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing key: %v", err)
	}
	if _, err := r.Resolve(context.Background(), "secret://file/nope/nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing file: %v", err)
	}
}

func TestResolver_FetchesEachSecretOnce(t *testing.T) {
	var opens, gets atomic.Int32
	unregisterAfter(t, "counting")
	Register("counting", func(context.Context) (Store, error) {
		opens.Add(1)
		return StoreFunc(func(context.Context, string) (string, error) {
			gets.Add(1)
			return `{"a":"1","b":"2"}`, nil
		}), nil
	})
	r := NewResolver()
	for _, k := range []string{"a", "b", "a"} {
		if _, err := r.Resolve(context.Background(), "secret://counting/db#"+k); err != nil {
			t.Fatal(err)
		}
	}
	if opens.Load() != 1 || gets.Load() != 1 {
		t.Errorf("opens=%d gets=%d, want 1/1", opens.Load(), gets.Load())
	}
}

func TestResolve_UnknownProviderNamesImport(t *testing.T) {
	_, err := Resolve(context.Background(), "secret://nope/x")
	if !errors.Is(err, ErrInvalidRef) {
		t.Fatalf("err=%v", err)
	}
}

// unregisterAfter removes a test provider so the test can run again (-count).
func unregisterAfter(t *testing.T, provider string) {
	t.Cleanup(func() {
		mu.Lock()
		delete(openers, provider)
		mu.Unlock()
	})
}

func TestRegister_RefusesExistingProvider(t *testing.T) {
	unregisterAfter(t, "dup-test")
	Register("dup-test", func(context.Context) (Store, error) { return StoreFunc(getEnv), nil })
	for _, name := range []string{"env", "file", "dup-test"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Register(%q) twice must panic", name)
				}
			}()
			Register(name, func(context.Context) (Store, error) { return nil, errors.New("hijacked") })
		}()
	}
	t.Setenv("GOFI_TEST_SECRET", "real")
	if v, err := Resolve(context.Background(), "secret://env/GOFI_TEST_SECRET"); v != "real" || err != nil {
		t.Errorf("built-in env provider was replaced: %q,%v", v, err)
	}
}

func TestRegister_RefusesEmptyArguments(t *testing.T) {
	for _, tc := range []struct {
		name string
		o    Opener
	}{{"", func(context.Context) (Store, error) { return nil, nil }}, {"nil-opener", nil}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Register(%q, %v) must panic", tc.name, tc.o == nil)
				}
			}()
			Register(tc.name, tc.o)
		}()
	}
}

func TestResolve_InvalidJSONDoesNotLeakSecret(t *testing.T) {
	// encoding/json quotes the offending byte ("invalid character 'Q' ...").
	const secret = "Qs3cret"
	path := filepath.Join(t.TempDir(), "raw")
	if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Resolve(context.Background(), "secret://file"+path+"#password")
	if !errors.Is(err, ErrInvalidRef) {
		t.Fatalf("err=%v, want ErrInvalidRef", err)
	}
	if strings.Contains(err.Error(), "'Q'") || strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaks the secret: %v", err)
	}
	if !strings.Contains(err.Error(), path[1:]) {
		t.Errorf("error should name the reference: %v", err)
	}
}
