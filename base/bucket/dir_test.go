package bucket_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/base/bucket"
	"github.com/joaoprofile/gofi-sdk-go/base/bucket/mem"
)

// plain hides every optional interface of the wrapped store.
type plain struct{ bucket.Store }

// lister answers ListDir with a fixed Dir.
type lister struct {
	bucket.Store
	dir bucket.Dir
}

func (l lister) ListDir(context.Context, string) (bucket.Dir, error) { return l.dir, nil }

// batcher counts DeleteMany calls.
type batcher struct {
	bucket.Store
	calls int
}

func (b *batcher) DeleteMany(ctx context.Context, keys []string) error {
	b.calls++
	for _, k := range keys {
		if err := b.Delete(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

func seed(t *testing.T, keys ...string) *mem.Store {
	t.Helper()
	s := mem.New("t")
	for _, k := range keys {
		if err := s.Put(context.Background(), bucket.PutInput{Key: k, Body: strings.NewReader("")}); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func keys(objs []bucket.Object) []string {
	out := []string{}
	for _, o := range objs {
		out = append(out, o.Key)
	}
	return out
}

func tree(t *testing.T) *mem.Store {
	return seed(t, "a.txt", "docs/", "docs/b.txt", "docs/sub/c.txt")
}

func TestListDir(t *testing.T) {
	ctx := context.Background()
	s := plain{tree(t)}
	for _, tc := range []struct {
		prefix           string
		folders, objects []string
	}{
		{"", []string{"docs/"}, []string{"a.txt"}},
		{"docs/", []string{"docs/sub/"}, []string{"docs/b.txt"}},
		{"docs/sub/", []string{}, []string{"docs/sub/c.txt"}},
		{"none/", []string{}, []string{}},
	} {
		d, err := bucket.ListDir(ctx, s, tc.prefix)
		if err != nil {
			t.Fatal(err)
		}
		if d.Prefix != tc.prefix || !slices.Equal(d.Folders, tc.folders) || !slices.Equal(keys(d.Objects), tc.objects) {
			t.Errorf("ListDir(%q)=%+v; want %v %v", tc.prefix, d, tc.folders, tc.objects)
		}
		if d.Folders == nil || d.Objects == nil {
			t.Errorf("ListDir(%q): nil slices", tc.prefix)
		}
	}
}

func TestListDir_UsesDirLister(t *testing.T) {
	ctx := context.Background()
	s := tree(t)
	want, err := bucket.ListDir(ctx, plain{s}, "docs/")
	if err != nil {
		t.Fatal(err)
	}
	got, err := bucket.ListDir(ctx, lister{Store: s, dir: want}, "docs/")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("DirLister result=%+v,%v; want %+v", got, err, want)
	}
}

func TestDeletePrefix(t *testing.T) {
	ctx := context.Background()
	for _, s := range []bucket.Store{plain{tree(t)}, tree(t)} {
		n, err := bucket.DeletePrefix(ctx, s, "docs/")
		if err != nil || n != 3 {
			t.Fatalf("%T: DeletePrefix=%d,%v; want 3", s, n, err)
		}
		d, err := bucket.ListDir(ctx, s, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(d.Folders) != 0 || !slices.Equal(keys(d.Objects), []string{"a.txt"}) {
			t.Errorf("%T: after DeletePrefix=%+v; want only a.txt", s, d)
		}
	}
}

func TestDeletePrefix_WholeBucket(t *testing.T) {
	s := tree(t)
	if n, err := bucket.DeletePrefix(context.Background(), s, ""); err != nil || n != 4 {
		t.Fatalf("DeletePrefix(\"\")=%d,%v; want 4", n, err)
	}
	if objs, _ := s.List(context.Background(), ""); len(objs) != 0 {
		t.Errorf("left %v", keys(objs))
	}
}

func TestDeletePrefix_Batches(t *testing.T) {
	ks := make([]string, 2500)
	for i := range ks {
		ks[i] = fmt.Sprintf("x/%04d", i)
	}
	b := &batcher{Store: seed(t, ks...)}
	n, err := bucket.DeletePrefix(context.Background(), b, "x/")
	if err != nil || n != 2500 || b.calls != 3 {
		t.Fatalf("DeletePrefix=%d,%v with %d batches; want 2500 in 3", n, err, b.calls)
	}
}

// failing fails every Delete.
type failing struct{ bucket.Store }

func (failing) Delete(context.Context, string) error { return errors.New("boom") }

func TestDeletePrefix_StopsAtFirstError(t *testing.T) {
	n, err := bucket.DeletePrefix(context.Background(), failing{tree(t)}, "")
	if err == nil || n != 0 {
		t.Fatalf("DeletePrefix=%d,%v; want the first error", n, err)
	}
}

func TestCreateFolder(t *testing.T) {
	ctx := context.Background()
	s := plain{tree(t)}
	if err := bucket.CreateFolder(ctx, s, "x"); err != nil {
		t.Fatal(err)
	}
	d, _ := bucket.ListDir(ctx, s, "")
	if !slices.Equal(d.Folders, []string{"docs/", "x/"}) {
		t.Errorf("Folders=%v; want docs/ and x/", d.Folders)
	}
	if err := bucket.CreateFolder(ctx, s, ""); !errors.Is(err, bucket.ErrInvalidConfig) {
		t.Errorf("empty path: %v", err)
	}
}

func TestStat_Fallback(t *testing.T) {
	ctx := context.Background()
	s := plain{seed(t, "k")}
	o, err := bucket.Stat(ctx, s, "k")
	if err != nil || o.Key != "k" {
		t.Fatalf("Stat=%+v,%v", o, err)
	}
	if _, err := bucket.Stat(ctx, s, "missing"); !errors.Is(err, bucket.ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
}
