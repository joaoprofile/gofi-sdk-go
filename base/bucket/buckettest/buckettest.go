// Package buckettest is the contract every bucket.Store must satisfy. Run it
// from each provider's tests so behaviour stays identical across backends.
package buckettest

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/base/bucket"
)

// Run exercises s; it must start empty and accept any key.
func Run(t *testing.T, s bucket.Store) {
	t.Helper()
	ctx := context.Background()

	put := func(t *testing.T, key, body string) {
		t.Helper()
		if err := s.Put(ctx, bucket.PutInput{Key: key, Body: strings.NewReader(body), Size: int64(len(body)), ContentType: "text/plain"}); err != nil {
			t.Fatalf("Put(%q): %v", key, err)
		}
	}

	t.Run("PutGetRoundTrip", func(t *testing.T) {
		put(t, "docs/a.txt", "hello")
		obj, rc, err := s.Get(ctx, "docs/a.txt")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		defer rc.Close()
		b, _ := io.ReadAll(rc)
		if string(b) != "hello" || obj.Key != "docs/a.txt" || obj.Size != 5 {
			t.Errorf("got %q %+v", b, obj)
		}
	})

	t.Run("PutOverwrites", func(t *testing.T) {
		put(t, "docs/over.txt", "one")
		put(t, "docs/over.txt", "two")
		_, rc, err := s.Get(ctx, "docs/over.txt")
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		if b, _ := io.ReadAll(rc); string(b) != "two" {
			t.Errorf("got %q", b)
		}
	})

	t.Run("PutUnknownSize", func(t *testing.T) {
		if err := s.Put(ctx, bucket.PutInput{Key: "docs/stream.txt", Body: strings.NewReader("streamed"), Size: -1}); err != nil {
			t.Fatalf("Put with unknown size: %v", err)
		}
	})

	t.Run("GetMissingIsNotFound", func(t *testing.T) {
		if _, _, err := s.Get(ctx, "missing/key"); !errors.Is(err, bucket.ErrNotFound) {
			t.Errorf("err=%v, want ErrNotFound", err)
		}
	})

	t.Run("PutValidates", func(t *testing.T) {
		if err := s.Put(ctx, bucket.PutInput{Body: strings.NewReader("x")}); !errors.Is(err, bucket.ErrInvalidConfig) {
			t.Errorf("empty key: %v", err)
		}
		if err := s.Put(ctx, bucket.PutInput{Key: "k"}); !errors.Is(err, bucket.ErrInvalidConfig) {
			t.Errorf("nil body: %v", err)
		}
	})

	t.Run("ListAndAllByPrefix", func(t *testing.T) {
		put(t, "list/1", "a")
		put(t, "list/2", "b")
		put(t, "other/3", "c")
		objs, err := s.List(ctx, "list/")
		if err != nil {
			t.Fatal(err)
		}
		if len(objs) != 2 {
			t.Fatalf("List=%v", objs)
		}
		var keys []string
		for o, err := range bucket.All(ctx, s, "list/") {
			if err != nil {
				t.Fatal(err)
			}
			keys = append(keys, o.Key)
		}
		if strings.Join(keys, ",") != "list/1,list/2" {
			t.Errorf("All keys=%v", keys)
		}
		for range bucket.All(ctx, s, "list/") {
			break // early stop must not panic
		}
		empty, err := s.List(ctx, "nothing-here/")
		if err != nil || empty == nil || len(empty) != 0 {
			t.Errorf("empty List=%v,%v; must be a non-nil empty slice", empty, err)
		}
	})

	t.Run("DeleteIsIdempotent", func(t *testing.T) {
		put(t, "del/x", "x")
		if err := s.Delete(ctx, "del/x"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Get(ctx, "del/x"); !errors.Is(err, bucket.ErrNotFound) {
			t.Errorf("deleted object still readable: %v", err)
		}
		if err := s.Delete(ctx, "del/x"); err != nil {
			t.Errorf("second delete: %v", err)
		}
	})

	t.Run("PresignGet", func(t *testing.T) {
		put(t, presignKey, "p")
		checkPresign(ctx, t, s)
	})

	t.Run("StatMatchesGet", func(t *testing.T) {
		put(t, "stat/a.txt", "hello")
		st, err := bucket.Stat(ctx, s, "stat/a.txt")
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		obj, rc, err := s.Get(ctx, "stat/a.txt")
		if err != nil {
			t.Fatal(err)
		}
		rc.Close()
		if st.Size != obj.Size || st.ContentType != obj.ContentType || !st.LastModified.Equal(obj.LastModified) {
			t.Errorf("Stat=%+v, Get=%+v", st, obj)
		}
		if _, err := bucket.Stat(ctx, s, "stat/missing"); !errors.Is(err, bucket.ErrNotFound) {
			t.Errorf("missing key: %v; want ErrNotFound", err)
		}
	})

	t.Run("ListDirOneLevel", func(t *testing.T) {
		seedTree(t, put, "dir1/")
		checkTree(ctx, t, s, "dir1/")
	})

	t.Run("CreateFolderShowsEmptyFolder", func(t *testing.T) {
		if err := bucket.CreateFolder(ctx, s, "dir2/new"); err != nil {
			t.Fatalf("CreateFolder: %v", err)
		}
		d, err := bucket.ListDir(ctx, s, "dir2/")
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(d.Folders, []string{"dir2/new/"}) || len(d.Objects) != 0 {
			t.Errorf("ListDir(dir2/)=%+v; want folder dir2/new/", d)
		}
		if err := bucket.CreateFolder(ctx, s, "/"); !errors.Is(err, bucket.ErrInvalidConfig) {
			t.Errorf("empty path: %v; want ErrInvalidConfig", err)
		}
	})

	t.Run("DeletePrefixRemovesSubtree", func(t *testing.T) {
		seedTree(t, put, "dir3/")
		n, err := bucket.DeletePrefix(ctx, s, "dir3/docs/")
		if err != nil {
			t.Fatalf("DeletePrefix: %v", err)
		}
		if n < 2 {
			t.Errorf("deleted %d objects; want at least the 2 files", n)
		}
		d, err := bucket.ListDir(ctx, s, "dir3/")
		if err != nil {
			t.Fatal(err)
		}
		if len(d.Folders) != 0 || len(d.Objects) != 1 || d.Objects[0].Key != "dir3/a.txt" {
			t.Errorf("after DeletePrefix ListDir(dir3/)=%+v; want only dir3/a.txt", d)
		}
	})
}

// seedTree writes, under base, a.txt, the docs/ marker, docs/b.txt and
// docs/sub/c.txt.
func seedTree(t *testing.T, put func(*testing.T, string, string), base string) {
	t.Helper()
	put(t, base+"a.txt", "a")
	put(t, base+"docs/", "")
	put(t, base+"docs/b.txt", "b")
	put(t, base+"docs/sub/c.txt", "c")
}

// checkTree checks two levels of the tree seedTree wrote under base.
func checkTree(ctx context.Context, t *testing.T, s bucket.Store, base string) {
	t.Helper()
	for _, lvl := range []struct {
		prefix  string
		folders []string
		objects []string
	}{
		{base, []string{base + "docs/"}, []string{base + "a.txt"}},
		{base + "docs/", []string{base + "docs/sub/"}, []string{base + "docs/b.txt"}},
	} {
		d, err := bucket.ListDir(ctx, s, lvl.prefix)
		if err != nil {
			t.Fatalf("ListDir(%q): %v", lvl.prefix, err)
		}
		if d.Folders == nil || d.Objects == nil {
			t.Errorf("ListDir(%q): Folders and Objects must be non-nil", lvl.prefix)
		}
		if !slices.Equal(d.Folders, lvl.folders) || !slices.Equal(keys(d.Objects), lvl.objects) {
			t.Errorf("ListDir(%q)=%v %v; want %v %v", lvl.prefix, d.Folders, keys(d.Objects), lvl.folders, lvl.objects)
		}
	}
}

func keys(objs []bucket.Object) []string {
	out := make([]string, 0, len(objs))
	for _, o := range objs {
		out = append(out, o.Key)
	}
	return out
}

// RunManager exercises m; the account must not hold a bucket named
// "gofi-buckettest" nor "gofi-buckettest-missing".
func RunManager(t *testing.T, m bucket.Manager) {
	t.Helper()
	ctx := context.Background()
	const name, missing = "gofi-buckettest", "gofi-buckettest-missing"

	listed := func(t *testing.T) bool {
		t.Helper()
		bs, err := m.ListBuckets(ctx)
		if err != nil {
			t.Fatalf("ListBuckets: %v", err)
		}
		if bs == nil {
			t.Fatal("ListBuckets returned nil; want a non-nil slice")
		}
		return slices.ContainsFunc(bs, func(b bucket.BucketInfo) bool { return b.Name == name })
	}

	t.Run("CreateBucket", func(t *testing.T) {
		if err := m.CreateBucket(ctx, name, bucket.CreateOptions{}); err != nil {
			t.Fatalf("CreateBucket: %v", err)
		}
		if !listed(t) {
			t.Errorf("%q missing from ListBuckets", name)
		}
	})

	t.Run("CreateBucketTwiceIsExists", func(t *testing.T) {
		if err := m.CreateBucket(ctx, name, bucket.CreateOptions{}); !errors.Is(err, bucket.ErrBucketExists) {
			t.Errorf("err=%v; want ErrBucketExists", err)
		}
	})

	t.Run("CreateBucketInvalidName", func(t *testing.T) {
		if err := m.CreateBucket(ctx, "A_b", bucket.CreateOptions{}); !errors.Is(err, bucket.ErrInvalidBucketName) {
			t.Errorf("err=%v; want ErrInvalidBucketName", err)
		}
	})

	t.Run("OpenAndListDir", func(t *testing.T) {
		s, err := m.Open(ctx, name)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		put := func(t *testing.T, key, body string) {
			t.Helper()
			if err := s.Put(ctx, bucket.PutInput{Key: key, Body: strings.NewReader(body), Size: int64(len(body))}); err != nil {
				t.Fatalf("Put(%q): %v", key, err)
			}
		}
		seedTree(t, put, "")
		checkTree(ctx, t, s, "")
	})

	t.Run("DeleteNonEmptyIsNotEmpty", func(t *testing.T) {
		if err := m.DeleteBucket(ctx, name, bucket.DeleteOptions{}); !errors.Is(err, bucket.ErrBucketNotEmpty) {
			t.Errorf("err=%v; want ErrBucketNotEmpty", err)
		}
	})

	t.Run("DeleteForce", func(t *testing.T) {
		if err := m.DeleteBucket(ctx, name, bucket.DeleteOptions{Force: true}); err != nil {
			t.Fatalf("DeleteBucket(Force): %v", err)
		}
		if listed(t) {
			t.Errorf("%q still in ListBuckets", name)
		}
	})

	t.Run("MissingBucketIsBucketNotFound", func(t *testing.T) {
		if _, err := m.Open(ctx, missing); !errors.Is(err, bucket.ErrBucketNotFound) {
			t.Errorf("Open: %v; want ErrBucketNotFound", err)
		}
		if err := m.DeleteBucket(ctx, missing, bucket.DeleteOptions{}); !errors.Is(err, bucket.ErrBucketNotFound) {
			t.Errorf("DeleteBucket: %v; want ErrBucketNotFound", err)
		}
	})
}

const presignKey = "share/p.txt"

// checkPresign expects presignKey to exist in s.
func checkPresign(ctx context.Context, t *testing.T, s bucket.Store) {
	t.Helper()
	u, err := s.PresignGet(ctx, presignKey, time.Minute)
	if err != nil || u == "" {
		t.Errorf("PresignGet=%q,%v", u, err)
	}
	if _, err := s.PresignGet(ctx, "", time.Minute); !errors.Is(err, bucket.ErrInvalidConfig) {
		t.Errorf("empty key: %v", err)
	}
	for _, ttl := range []time.Duration{0, -time.Minute, bucket.MaxPresignTTL + time.Second} {
		if _, err := s.PresignGet(ctx, presignKey, ttl); !errors.Is(err, bucket.ErrInvalidTTL) {
			t.Errorf("ttl %s: %v; want ErrInvalidTTL", ttl, err)
		}
	}
}
