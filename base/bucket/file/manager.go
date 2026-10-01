package file

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/joaoprofile/gofi-sdk-go/base/bucket"
)

// Manager is a bucket.Manager over a directory: each bucket is a
// subdirectory whose name passes bucket.ValidateName. Safe for concurrent use.
type Manager struct {
	dir string

	mu     sync.Mutex
	stores map[string]*Store
}

var _ bucket.Manager = (*Manager)(nil)

// NewManager uses dir (created when missing) as the parent of the buckets.
func NewManager(dir string) (*Manager, error) {
	if dir == "" {
		return nil, fmt.Errorf("%w: directory is required", bucket.ErrInvalidConfig)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("file bucket: %w", err)
	}
	return &Manager{dir: dir, stores: make(map[string]*Store)}, nil
}

// Close releases the directory handles of the opened stores.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var errs []error
	for name, s := range m.stores {
		errs = append(errs, s.Close())
		delete(m.stores, name)
	}
	return errors.Join(errs...)
}

// ListBuckets returns the subdirectories with a valid bucket name, in name
// order; CreatedAt is the directory's modification time and Region is empty.
func (m *Manager) ListBuckets(context.Context) ([]bucket.BucketInfo, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil, fmt.Errorf("file bucket: list buckets: %w", err)
	}
	out := make([]bucket.BucketInfo, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || bucket.ValidateName(e.Name()) != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, fmt.Errorf("file bucket: list buckets: %w", err)
		}
		out = append(out, bucket.BucketInfo{Name: e.Name(), CreatedAt: info.ModTime()})
	}
	return out, nil
}

// CreateBucket ignores opts.Region.
func (m *Manager) CreateBucket(_ context.Context, name string, _ bucket.CreateOptions) error {
	if err := bucket.ValidateName(name); err != nil {
		return err
	}
	if err := os.Mkdir(filepath.Join(m.dir, name), 0o750); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%w: %q", bucket.ErrBucketExists, name)
		}
		return fmt.Errorf("file bucket: create bucket %q: %w", name, err)
	}
	return nil
}

func (m *Manager) DeleteBucket(ctx context.Context, name string, opts bucket.DeleteOptions) error {
	s, err := m.store(ctx, name)
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
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("file bucket: delete bucket %q: %w", name, err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("%w: %q", bucket.ErrBucketNotEmpty, name)
	}
	delete(m.stores, name)
	s.Close()
	if err := os.Remove(s.dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("file bucket: delete bucket %q: %w", name, err)
	}
	return nil
}

func (m *Manager) Open(ctx context.Context, name string) (bucket.Store, error) {
	return m.store(ctx, name)
}

func (m *Manager) store(_ context.Context, name string) (*Store, error) {
	if err := bucket.ValidateName(name); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.stores[name]; ok {
		return s, nil
	}
	dir := filepath.Join(m.dir, name)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %q", bucket.ErrBucketNotFound, name)
		}
		return nil, fmt.Errorf("file bucket: open bucket %q: %w", name, err)
	}
	s, err := New(dir)
	if err != nil {
		return nil, err
	}
	m.stores[name] = s
	return s, nil
}
