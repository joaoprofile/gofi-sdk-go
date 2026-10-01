package mem

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/base/bucket"
)

// Manager is an in-memory bucket.Manager; each bucket is a Store. Safe for
// concurrent use.
type Manager struct {
	mu      sync.RWMutex
	buckets map[string]*entry
}

type entry struct {
	store   *Store
	created time.Time
}

var _ bucket.Manager = (*Manager)(nil)

// NewManager returns a Manager without buckets.
func NewManager() *Manager {
	return &Manager{buckets: make(map[string]*entry)}
}

// ListBuckets returns the buckets in name order; Region is empty.
func (m *Manager) ListBuckets(context.Context) ([]bucket.BucketInfo, error) {
	m.mu.RLock()
	out := make([]bucket.BucketInfo, 0, len(m.buckets))
	for name, e := range m.buckets {
		out = append(out, bucket.BucketInfo{Name: name, CreatedAt: e.created})
	}
	m.mu.RUnlock()
	slices.SortFunc(out, func(a, b bucket.BucketInfo) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// CreateBucket ignores opts.Region.
func (m *Manager) CreateBucket(_ context.Context, name string, _ bucket.CreateOptions) error {
	if err := bucket.ValidateName(name); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.buckets[name]; ok {
		return fmt.Errorf("%w: %q", bucket.ErrBucketExists, name)
	}
	m.buckets[name] = &entry{store: New(name), created: time.Now()}
	return nil
}

func (m *Manager) DeleteBucket(ctx context.Context, name string, opts bucket.DeleteOptions) error {
	s, err := m.store(name)
	if err != nil {
		return err
	}
	if opts.Force {
		if _, err := bucket.DeletePrefix(ctx, s, ""); err != nil {
			return err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !s.empty() {
		return fmt.Errorf("%w: %q", bucket.ErrBucketNotEmpty, name)
	}
	delete(m.buckets, name)
	return nil
}

func (m *Manager) Open(_ context.Context, name string) (bucket.Store, error) {
	return m.store(name)
}

func (m *Manager) store(name string) (*Store, error) {
	m.mu.RLock()
	e, ok := m.buckets[name]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %q", bucket.ErrBucketNotFound, name)
	}
	return e.store, nil
}
