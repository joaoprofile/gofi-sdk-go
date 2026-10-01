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
