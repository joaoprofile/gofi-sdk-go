// Package secrets resolves secret references of the form
//
//	secret://<provider>/<name>[#<json-key>]
//
// e.g. secret://awssm/prod/db#password. gofi's environment loader resolves
// any variable holding a reference at startup, so credentials stay in the
// secret manager instead of the deployment manifest. Providers register
// themselves: "env" and "file" are built in; awssm and ocivault live in
// their own modules.
package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

// Prefix starts every secret reference.
const Prefix = "secret://"

var (
	// ErrInvalidRef is returned for malformed references or unknown providers.
	ErrInvalidRef = errors.New("secrets: invalid reference")
	// ErrNotFound is returned when the secret or its JSON key does not exist.
	ErrNotFound = errors.New("secrets: not found")
)

// Store fetches the raw value of a secret by provider-specific name.
type Store interface {
	Get(ctx context.Context, name string) (string, error)
}

// StoreFunc adapts a function to Store.
type StoreFunc func(ctx context.Context, name string) (string, error)

func (f StoreFunc) Get(ctx context.Context, name string) (string, error) { return f(ctx, name) }

// Opener creates a provider's Store on first use.
type Opener func(ctx context.Context) (Store, error)

var (
	mu      sync.RWMutex
	openers = map[string]Opener{
		"env":  func(context.Context) (Store, error) { return StoreFunc(getEnv), nil },
		"file": func(context.Context) (Store, error) { return StoreFunc(readFile), nil },
	}
)

// Register makes a provider available to references. Like sql.Register, it
// panics when provider is empty, o is nil or the provider is already
// registered, so an imported package cannot silently replace a secret source.
func Register(provider string, o Opener) {
	if provider == "" || o == nil {
		panic("secrets: Register needs a provider name and an opener")
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := openers[provider]; dup {
		panic("secrets: Register called twice for provider " + provider)
	}
	openers[provider] = o
}

// Ref is a parsed secret reference.
type Ref struct {
	Provider string
	Name     string
	Key      string // optional field of a JSON secret
}

// IsRef reports whether s is a secret reference.
func IsRef(s string) bool { return strings.HasPrefix(s, Prefix) }

// ParseRef parses secret://<provider>/<name>[#<key>].
func ParseRef(s string) (Ref, error) {
	rest, ok := strings.CutPrefix(s, Prefix)
	if !ok {
		return Ref{}, fmt.Errorf("%w: %q lacks %s", ErrInvalidRef, s, Prefix)
	}
	rest, key, _ := strings.Cut(rest, "#")
	provider, name, _ := strings.Cut(rest, "/")
	if provider == "" || name == "" {
		return Ref{}, fmt.Errorf("%w: %q needs a provider and a name", ErrInvalidRef, s)
	}
	return Ref{Provider: provider, Name: name, Key: key}, nil
}

// Resolver resolves references, opening each provider and fetching each
// secret once. It is safe for concurrent use.
type Resolver struct {
	mu     sync.Mutex
	stores map[string]Store
	values map[string]string
}

// NewResolver returns an empty Resolver.
func NewResolver() *Resolver {
	return &Resolver{stores: map[string]Store{}, values: map[string]string{}}
}

// Resolve returns s unchanged unless it is a reference.
func (r *Resolver) Resolve(ctx context.Context, s string) (string, error) {
	if !IsRef(s) {
		return s, nil
	}
	ref, err := ParseRef(s)
	if err != nil {
		return "", err
	}
	raw, err := r.fetch(ctx, ref)
	if err != nil {
		return "", err
	}
	if ref.Key == "" {
		return raw, nil
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		// The decoder error may quote part of the secret, so it is dropped.
		return "", fmt.Errorf("%w: %s/%s is not a JSON object", ErrInvalidRef, ref.Provider, ref.Name)
	}
	v, ok := fields[ref.Key]
	if !ok {
		return "", fmt.Errorf("%w: key %q in %s/%s", ErrNotFound, ref.Key, ref.Provider, ref.Name)
	}
	if str, ok := v.(string); ok {
		return str, nil
	}
	b, _ := json.Marshal(v)
	return string(b), nil
}

func (r *Resolver) fetch(ctx context.Context, ref Ref) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := ref.Provider + "/" + ref.Name
	if v, ok := r.values[id]; ok {
		return v, nil
	}
	store, ok := r.stores[ref.Provider]
	if !ok {
		mu.RLock()
		open, found := openers[ref.Provider]
		mu.RUnlock()
		if !found {
			return "", fmt.Errorf("%w: provider %q is not registered; import _ \"github.com/joaoprofile/gofi-sdk-go/base/secrets/%s\"",
				ErrInvalidRef, ref.Provider, ref.Provider)
		}
		var err error
		if store, err = open(ctx); err != nil {
			return "", fmt.Errorf("secrets: open %s: %w", ref.Provider, err)
		}
		r.stores[ref.Provider] = store
	}
	v, err := store.Get(ctx, ref.Name)
	if err != nil {
		return "", fmt.Errorf("secrets: get %s: %w", id, err)
	}
	r.values[id] = v
	return v, nil
}

// Resolve resolves a single reference with a fresh Resolver.
func Resolve(ctx context.Context, s string) (string, error) {
	return NewResolver().Resolve(ctx, s)
}

func getEnv(_ context.Context, name string) (string, error) {
	v, ok := os.LookupEnv(name)
	if !ok {
		return "", fmt.Errorf("%w: env %s", ErrNotFound, name)
	}
	return v, nil
}

// readFile reads secret://file/<absolute path without the leading slash>.
func readFile(_ context.Context, name string) (string, error) {
	b, err := os.ReadFile("/" + strings.TrimPrefix(name, "/"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("%w: %w", ErrNotFound, err)
		}
		return "", err
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}
