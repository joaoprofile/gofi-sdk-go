// Package file implements bucket.Store on a local directory, for development
// and single-node deployments. Keys map to paths below the root and cannot
// escape it. Importing it registers the "file" provider for bucket.Open
// (Config.Endpoint is the directory) and bucket.OpenManager (Config.Endpoint
// is the parent directory, one subdirectory per bucket). ContentType is
// derived from the key extension.
//
// Folders are directories: a "path/" marker key creates one, an empty
// directory lists as its "path/" marker, and a non-empty one is implied by
// the keys below it.
package file

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"mime"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/base/bucket"
)

func init() {
	bucket.Register(bucket.ProviderFile, func(_ context.Context, cfg bucket.Config) (bucket.Store, error) {
		return New(cfg.Endpoint)
	})
	bucket.RegisterManager(bucket.ProviderFile, func(_ context.Context, cfg bucket.Config) (bucket.Manager, error) {
		return NewManager(cfg.Endpoint)
	})
}

// Store is a bucket.Store rooted at a directory.
type Store struct {
	dir  string
	root *os.Root
}

var (
	_ bucket.Store       = (*Store)(nil)
	_ bucket.Walker      = (*Store)(nil)
	_ bucket.FolderMaker = (*Store)(nil)
	_ bucket.Statter     = (*Store)(nil)
)

// New opens (creating when missing) the root directory.
func New(dir string) (*Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("%w: directory is required", bucket.ErrInvalidConfig)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("file bucket: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("file bucket: %w", err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("file bucket: %w", err)
	}
	return &Store{dir: abs, root: root}, nil
}

// Close releases the root directory handle.
func (s *Store) Close() error { return s.root.Close() }

func name(key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("%w: key is required", bucket.ErrInvalidConfig)
	}
	return filepath.FromSlash(strings.TrimPrefix(key, "/")), nil
}

// Put writes to a temporary file and renames it, so readers never see a
// partial object. A key ending in "/" is a folder marker: it creates the
// directory and its body must be empty.
func (s *Store) Put(ctx context.Context, in bucket.PutInput) error {
	n, err := name(in.Key)
	if err != nil {
		return err
	}
	if in.Body == nil {
		return fmt.Errorf("%w: body is required", bucket.ErrInvalidConfig)
	}
	if strings.HasSuffix(in.Key, "/") {
		var b [1]byte
		if k, err := io.ReadFull(in.Body, b[:]); k > 0 {
			return fmt.Errorf("%w: folder marker %q must have an empty body", bucket.ErrInvalidConfig, in.Key)
		} else if !errors.Is(err, io.EOF) {
			return fmt.Errorf("file bucket: put %q: %w", in.Key, err)
		}
		return s.CreateFolder(ctx, in.Key)
	}
	if dir := filepath.Dir(n); dir != "." {
		if err := s.root.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("file bucket: put %q: %w", in.Key, err)
		}
	}
	tmp := n + ".tmp-" + fmt.Sprint(time.Now().UnixNano())
	f, err := s.root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return fmt.Errorf("file bucket: put %q: %w", in.Key, err)
	}
	_, err = io.Copy(f, in.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = s.root.Rename(tmp, n)
	}
	if err != nil {
		_ = s.root.Remove(tmp)
		return fmt.Errorf("file bucket: put %q: %w", in.Key, err)
	}
	return nil
}

func (s *Store) Get(_ context.Context, key string) (bucket.Object, io.ReadCloser, error) {
	n, err := name(key)
	if err != nil {
		return bucket.Object{}, nil, err
	}
	f, err := s.root.Open(n)
	if err != nil {
		return bucket.Object{}, nil, mapErr(key, err)
	}
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		f.Close()
		return bucket.Object{}, nil, fmt.Errorf("%w: %q", bucket.ErrNotFound, key)
	}
	return object(key, st), f, nil
}

func (s *Store) Stat(_ context.Context, key string) (bucket.Object, error) {
	n, err := name(key)
	if err != nil {
		return bucket.Object{}, err
	}
	st, err := s.root.Stat(n)
	if err != nil {
		return bucket.Object{}, mapErr(key, err)
	}
	if st.IsDir() {
		return bucket.Object{}, fmt.Errorf("%w: %q", bucket.ErrNotFound, key)
	}
	return object(key, st), nil
}

// CreateFolder creates the directory of path ("a/b" or "a/b/").
func (s *Store) CreateFolder(_ context.Context, path string) error {
	n, err := name(strings.TrimSuffix(path, "/"))
	if err != nil {
		return err
	}
	if err := s.root.MkdirAll(n, 0o750); err != nil {
		return fmt.Errorf("file bucket: create folder %q: %w", path, err)
	}
	return nil
}

// All walks the directory in lexical order. An empty directory yields its
// "dir/" folder marker.
func (s *Store) All(_ context.Context, prefix string) iter.Seq2[bucket.Object, error] {
	return func(yield func(bucket.Object, error) bool) {
		fsys := s.root.FS()
		err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if strings.Contains(path.Base(p), ".tmp-") {
				return nil
			}
			key := p
			if d.IsDir() {
				if p == "." {
					return nil
				}
				key += "/"
			}
			if !strings.HasPrefix(key, prefix) {
				return nil
			}
			if d.IsDir() {
				if entries, err := fs.ReadDir(fsys, p); err != nil || len(entries) > 0 {
					return err
				}
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if !yield(object(key, info), nil) {
				return fs.SkipAll
			}
			return nil
		})
		if err != nil && !errors.Is(err, fs.SkipAll) {
			yield(bucket.Object{}, fmt.Errorf("file bucket: list %q: %w", prefix, err))
		}
	}
}

func (s *Store) List(ctx context.Context, prefix string) ([]bucket.Object, error) {
	return bucket.Collect(s.All(ctx, prefix))
}

// Delete removes the file of key. For a folder marker ("dir/") it removes the
// directory when empty and is a no-op otherwise, since the folder still
// exists through the keys below it.
func (s *Store) Delete(_ context.Context, key string) error {
	n, err := name(key)
	if err != nil {
		return err
	}
	if strings.HasSuffix(key, "/") {
		entries, err := fs.ReadDir(s.root.FS(), filepath.ToSlash(filepath.Clean(n)))
		if err != nil || len(entries) > 0 {
			return nil
		}
	}
	if err := s.root.Remove(n); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("file bucket: delete %q: %w", key, err)
	}
	return nil
}

// PresignGet returns a file:// URL; it is only readable on this host.
func (s *Store) PresignGet(_ context.Context, key string, ttl time.Duration) (string, error) {
	n, err := name(key)
	if err != nil {
		return "", err
	}
	if err := bucket.CheckPresignTTL(ttl, 0); err != nil {
		return "", err
	}
	if _, err := s.root.Stat(n); err != nil {
		return "", mapErr(key, err)
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(s.dir, n))}).String(), nil
}

func object(key string, info fs.FileInfo) bucket.Object {
	if info.IsDir() {
		return bucket.Object{Key: filepath.ToSlash(key), LastModified: info.ModTime()}
	}
	return bucket.Object{
		Key:          filepath.ToSlash(key),
		Size:         info.Size(),
		ContentType:  mime.TypeByExtension(path.Ext(key)),
		LastModified: info.ModTime(),
	}
}

func mapErr(key string, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %q", bucket.ErrNotFound, key)
	}
	return fmt.Errorf("file bucket: %q: %w", key, err)
}
