package file_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofi-labs/gofi-sdk-go/base/bucket"
	"github.com/gofi-labs/gofi-sdk-go/base/bucket/buckettest"
	"github.com/gofi-labs/gofi-sdk-go/base/bucket/file"
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
