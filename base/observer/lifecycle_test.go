package observer

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

// --- fakes ---

type fakeContextCloser struct {
	called bool
	err    error
}

func (f *fakeContextCloser) Close(_ context.Context) error {
	f.called = true
	return f.err
}

type fakeIOCloser struct {
	called bool
	err    error
}

func (f *fakeIOCloser) Close() error {
	f.called = true
	return f.err
}

type unknownCloser struct {
	called bool
}

// unknownCloser satisfies neither Closer nor io.Closer — must be silently skipped.

// --- tests ---

func TestNewRegistry(t *testing.T) {
	r := New()
	if r == nil {
		t.Fatal("New() returned nil")
	}
	if len(r.closers) != 0 {
		t.Errorf("Expected empty closers, got %d", len(r.closers))
	}
}

func TestRegistryRegister(t *testing.T) {
	r := New()
	c1 := &fakeContextCloser{}
	c2 := &fakeIOCloser{}
	r.Register(c1, c2)

	if len(r.closers) != 2 {
		t.Errorf("Expected 2 closers after Register, got %d", len(r.closers))
	}
}

func TestCloseAllContextCloser(t *testing.T) {
	r := New()
	c := &fakeContextCloser{}
	r.Register(c)

	if err := r.CloseAll(context.Background()); err != nil {
		t.Fatalf("CloseAll returned unexpected error: %v", err)
	}
	if !c.called {
		t.Error("Expected Closer.Close to be called")
	}
}

func TestCloseAllIOCloser(t *testing.T) {
	r := New()
	c := &fakeIOCloser{}
	r.Register(c)

	if err := r.CloseAll(context.Background()); err != nil {
		t.Fatalf("CloseAll returned unexpected error: %v", err)
	}
	if !c.called {
		t.Error("Expected io.Closer.Close to be called")
	}
}

func TestCloseAllUnknownTypeSkipped(t *testing.T) {
	r := New()
	u := &unknownCloser{}
	r.Register(u)

	// Should not panic and should return nil
	if err := r.CloseAll(context.Background()); err != nil {
		t.Fatalf("CloseAll returned unexpected error for unknown type: %v", err)
	}
}

func TestCloseAllContextCloserError(t *testing.T) {
	r := New()
	want := errors.New("close failed")
	c := &fakeContextCloser{err: want}
	r.Register(c)

	err := r.CloseAll(context.Background())
	if err == nil {
		t.Fatal("Expected error from CloseAll, got nil")
	}
	if !errors.Is(err, want) {
		t.Errorf("Expected error %v, got %v", want, err)
	}
}

func TestCloseAllIOCloserError(t *testing.T) {
	r := New()
	want := errors.New("io close failed")
	c := &fakeIOCloser{err: want}
	r.Register(c)

	err := r.CloseAll(context.Background())
	if err == nil {
		t.Fatal("Expected error from CloseAll, got nil")
	}
	if !errors.Is(err, want) {
		t.Errorf("Expected error %v, got %v", want, err)
	}
}

func TestCloseWithTimeout(t *testing.T) {
	r := New()
	c := &fakeContextCloser{}
	r.Register(c)

	if err := r.CloseWithTimeout(5 * time.Second); err != nil {
		t.Fatalf("CloseWithTimeout returned unexpected error: %v", err)
	}
	if !c.called {
		t.Error("Expected Closer.Close to be called by CloseWithTimeout")
	}
}

func TestCloseAllMultipleClosers(t *testing.T) {
	r := New()
	cc := &fakeContextCloser{}
	ic := &fakeIOCloser{}
	uc := &unknownCloser{}
	r.Register(cc, ic, uc)

	if err := r.CloseAll(context.Background()); err != nil {
		t.Fatalf("CloseAll returned unexpected error: %v", err)
	}
	if !cc.called {
		t.Error("Expected context Closer to be called")
	}
	if !ic.called {
		t.Error("Expected io.Closer to be called")
	}
}

// Ensure fakeIOCloser satisfies io.Closer at compile time.
var _ io.Closer = (*fakeIOCloser)(nil)
