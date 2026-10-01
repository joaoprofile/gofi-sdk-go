// Package mem implements bucket.Store in memory, for tests and local runs.
// Importing it registers the "mem" provider for bucket.Open.
package mem

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"iter"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/base/bucket"
)

func init() {
	bucket.Register(bucket.ProviderMem, func(_ context.Context, cfg bucket.Config) (bucket.Store, error) {
		return New(cfg.Name), nil
	})
}

type object struct {
	data []byte
	meta bucket.Object
}

// Store is an in-memory bucket.Store safe for concurrent use.
type Store struct {
	name    string
	mu      sync.RWMutex
	objects map[string]object
}

var (
	_ bucket.Store  = (*Store)(nil)
	_ bucket.Walker = (*Store)(nil)
)

// New returns an empty Store; name only appears in presigned URLs.
func New(name string) *Store {
	return &Store{name: name, objects: make(map[string]object)}
}

func (s *Store) Put(_ context.Context, in bucket.PutInput) error {
	if in.Key == "" || in.Body == nil {
		return fmt.Errorf("%w: key and body are required", bucket.ErrInvalidConfig)
	}
	data, err := io.ReadAll(in.Body)
	if err != nil {
		return fmt.Errorf("mem bucket: put %q: %w", in.Key, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[in.Key] = object{data: data, meta: bucket.Object{
		Key: in.Key, Size: int64(len(data)), ContentType: in.ContentType, LastModified: time.Now(),
	}}
	return nil
}

func (s *Store) Get(_ context.Context, key string) (bucket.Object, io.ReadCloser, error) {
	s.mu.RLock()
	o, ok := s.objects[key]
	s.mu.RUnlock()
	if !ok {
		return bucket.Object{}, nil, fmt.Errorf("%w: %q", bucket.ErrNotFound, key)
	}
	return o.meta, io.NopCloser(bytes.NewReader(o.data)), nil
}

// All yields objects in key order.
func (s *Store) All(_ context.Context, prefix string) iter.Seq2[bucket.Object, error] {
	return func(yield func(bucket.Object, error) bool) {
		s.mu.RLock()
		objs := make([]bucket.Object, 0, len(s.objects))
		for k, o := range s.objects {
			if strings.HasPrefix(k, prefix) {
				objs = append(objs, o.meta)
			}
		}
		s.mu.RUnlock()
		slices.SortFunc(objs, func(a, b bucket.Object) int { return strings.Compare(a.Key, b.Key) })
		for _, o := range objs {
			if !yield(o, nil) {
				return
			}
		}
	}
}

func (s *Store) List(ctx context.Context, prefix string) ([]bucket.Object, error) {
	return bucket.Collect(s.All(ctx, prefix))
}

func (s *Store) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	return nil
}

// PresignGet returns a mem:// URL; it is not downloadable.
func (s *Store) PresignGet(_ context.Context, key string, ttl time.Duration) (string, error) {
	if key == "" {
		return "", fmt.Errorf("%w: key is required", bucket.ErrInvalidConfig)
	}
	if err := bucket.CheckPresignTTL(ttl, 0); err != nil {
		return "", err
	}
	u := url.URL{Scheme: "mem", Host: s.name, Path: "/" + key,
		RawQuery: url.Values{"expires": {time.Now().Add(ttl).UTC().Format(time.RFC3339)}}.Encode()}
	return u.String(), nil
}
