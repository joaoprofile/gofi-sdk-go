// Package buckettest is the contract every bucket.Store must satisfy. Run it
// from each provider's tests so behaviour stays identical across backends.
package buckettest

import (
	"context"
	"errors"
	"io"
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
