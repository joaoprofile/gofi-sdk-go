package bucket

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestOpen(t *testing.T) {
	if _, err := Open(context.Background(), Config{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("unset provider: %v", err)
	}
	_, err := Open(context.Background(), Config{Provider: "nope"})
	if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), "import _") {
		t.Fatalf("unregistered provider must explain the import: %v", err)
	}

	called := false
	Register("fake", func(context.Context, Config) (Store, error) { called = true; return nil, nil })
	if _, err := Open(context.Background(), Config{Provider: "fake"}); err != nil || !called {
		t.Fatalf("registered opener not used: %v", err)
	}
}

func TestOpenManager(t *testing.T) {
	ctx := context.Background()
	if _, err := OpenManager(ctx, Config{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("unset provider: %v", err)
	}
	_, err := OpenManager(ctx, Config{Provider: "nope"})
	if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), "import _") {
		t.Fatalf("unregistered provider must explain the import: %v", err)
	}

	Register("storeonly", func(context.Context, Config) (Store, error) { return nil, nil })
	if _, err := OpenManager(ctx, Config{Provider: "storeonly"}); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("provider without manager: %v; want ErrNotSupported", err)
	}

	called := false
	Register("withmanager", func(context.Context, Config) (Store, error) { return nil, nil })
	RegisterManager("withmanager", func(_ context.Context, cfg Config) (Manager, error) {
		called = cfg.Name == ""
		return nil, nil
	})
	if _, err := OpenManager(ctx, Config{Provider: "withmanager"}); err != nil || !called {
		t.Fatalf("registered manager opener not used: %v", err)
	}
}
