package file_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/base/bucket"
	"github.com/joaoprofile/gofi-sdk-go/base/bucket/buckettest"
	"github.com/joaoprofile/gofi-sdk-go/base/bucket/file"
)

func newStore(t *testing.T) (*file.Store, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "objects")
	s, err := file.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, dir
}

func TestContract(t *testing.T) {
	s, _ := newStore(t)
	buckettest.Run(t, s)
}

// Keys cannot escape the root directory.
func TestKeysStayInsideRoot(t *testing.T) {
	s, dir := newStore(t)
	outside := filepath.Join(filepath.Dir(dir), "escaped")
	err := s.Put(context.Background(), bucket.PutInput{Key: "../escaped", Body: strings.NewReader("x")})
	if err == nil {
		t.Fatal("path traversal must fail")
	}
	if _, statErr := os.Stat(outside); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("file written outside the root")
	}
	if _, _, err := s.Get(context.Background(), "../../etc/passwd"); err == nil {
		t.Fatal("reading outside the root must fail")
	}
}

func TestContentTypeFromExtension(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if err := s.Put(ctx, bucket.PutInput{Key: "a.json", Body: strings.NewReader("{}")}); err != nil {
		t.Fatal(err)
	}
	obj, rc, err := s.Get(ctx, "a.json")
	if err != nil {
		t.Fatal(err)
	}
	rc.Close()
	if obj.ContentType != "application/json" {
		t.Errorf("ContentType=%q", obj.ContentType)
	}
}

func TestOpenURL(t *testing.T) {
	dir := t.TempDir()
	s, err := bucket.OpenURL(context.Background(), "file://"+filepath.ToSlash(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer s.(*file.Store).Close()
}

func TestManagerContract(t *testing.T) {
	m, err := file.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	buckettest.RunManager(t, m)
}

func TestOpenManager(t *testing.T) {
	m, err := bucket.OpenManager(context.Background(), bucket.Config{Provider: bucket.ProviderFile, Endpoint: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	m.(*file.Manager).Close()
}

func TestFolderMarker(t *testing.T) {
	s, dir := newStore(t)
	ctx := context.Background()
	if err := s.Put(ctx, bucket.PutInput{Key: "f/", Body: strings.NewReader(""), Size: -1}); err != nil {
		t.Fatalf("empty marker: %v", err)
	}
	if st, err := os.Stat(filepath.Join(dir, "f")); err != nil || !st.IsDir() {
		t.Fatalf("marker did not create a directory: %v", err)
	}
	if err := s.Put(ctx, bucket.PutInput{Key: "g/", Body: strings.NewReader("x")}); !errors.Is(err, bucket.ErrInvalidConfig) {
		t.Errorf("marker with body: %v; want ErrInvalidConfig", err)
	}
	if err := s.Put(ctx, bucket.PutInput{Key: "f/a.txt", Body: strings.NewReader("a")}); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "f/"); err != nil {
		t.Fatalf("delete of a non-empty folder must be a no-op: %v", err)
	}
	if _, _, err := s.Get(ctx, "f/a.txt"); err != nil {
		t.Fatalf("delete of the folder removed its content: %v", err)
	}
	if err := s.Delete(ctx, "f/a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "f/"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "f")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("empty folder not removed: %v", err)
	}
}

func TestManagerRejectsTraversal(t *testing.T) {
	m, err := file.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	ctx := context.Background()
	for _, n := range []string{"..", "../x", "a/b"} {
		if _, err := m.Open(ctx, n); !errors.Is(err, bucket.ErrInvalidBucketName) {
			t.Errorf("Open(%q)=%v", n, err)
		}
		if err := m.DeleteBucket(ctx, n, bucket.DeleteOptions{Force: true}); !errors.Is(err, bucket.ErrInvalidBucketName) {
			t.Errorf("DeleteBucket(%q)=%v", n, err)
		}
	}
}
