package cloud

import (
	"errors"
	"fmt"
)

// Adapter is the generic interface for type-safe retrieval of a cloud session.
// T is the SDK-specific session or client type returned by the provider.
type Adapter[T any] interface {
	Session() (T, error)
}

// TypedAdapter wraps a Provider and extracts its underlying session with a
// compile-time type parameter, eliminating explicit type assertions at call sites.
type TypedAdapter[T any] struct {
	provider Provider
}

// NewTypedAdapter returns a TypedAdapter bound to the given Provider.
func NewTypedAdapter[T any](p Provider) *TypedAdapter[T] {
	return &TypedAdapter[T]{provider: p}
}

// Session returns the provider session asserted to T.
// Errors are returned — not panicked — when:
//   - the provider is nil
//   - the session has not been initialised (Bootstrap not called or failed)
//   - the session's concrete type does not match T
func (a *TypedAdapter[T]) Session() (T, error) {
	var zero T
	if a.provider == nil {
		return zero, errors.New("cloud: adapter has no provider")
	}
	raw := a.provider.GetSession()
	if raw == nil {
		return zero, errors.New("cloud: provider session not initialised — call Bootstrap first")
	}
	typed, ok := raw.(T)
	if !ok {
		return zero, fmt.Errorf("cloud: session type mismatch: want %T, got %T", zero, raw)
	}
	return typed, nil
}

// SessionAs is a package-level shortcut that extracts a typed session directly
// from a Cloud instance, avoiding explicit TypedAdapter construction.
//
// Usage:
//
//	sess, err := cloud.SessionAs[*session.Session](c)
func SessionAs[T any](c *Cloud) (T, error) {
	return NewTypedAdapter[T](c.provider).Session()
}
