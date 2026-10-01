package s3

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/base/bucket"
	"github.com/gofi-labs/gofi-sdk-go/base/bucket/buckettest"
	cloudaws "github.com/gofi-labs/gofi-sdk-go/base/cloud/aws"
)

// fakeS3 is a minimal path-style S3 endpoint backed by a map.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strings.TrimPrefix(r.URL.Path, "/bkt/")
	switch r.Method {
	case http.MethodPut:
		b, _ := io.ReadAll(r.Body)
		f.objects[key] = b
		w.Header().Set("ETag", `"etag"`)
	case http.MethodGet:
		if r.URL.Path == "/bkt" || r.URL.Query().Get("list-type") == "2" {
			f.list(w, r.URL.Query())
			return
		}
		b, ok := f.objects[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `<Error><Code>NoSuchKey</Code></Error>`)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		w.Write(b)
	case http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	}
}

// list pages two keys at a time so paginated listing is exercised.
func (f *fakeS3) list(w http.ResponseWriter, q url.Values) {
	keys := make([]string, 0, len(f.objects))
	for k := range f.objects {
		if strings.HasPrefix(k, q.Get("prefix")) && k > q.Get("continuation-token") {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	truncated := len(keys) > 2
	if truncated {
		keys = keys[:2]
	}
	w.Header().Set("Content-Type", "application/xml")
	io.WriteString(w, "<ListBucketResult><IsTruncated>"+strconv.FormatBool(truncated)+"</IsTruncated>")
	if truncated {
		io.WriteString(w, "<NextContinuationToken>"+keys[1]+"</NextContinuationToken>")
	}
	for _, k := range keys {
		io.WriteString(w, "<Contents><Key>"+k+"</Key><Size>"+strconv.Itoa(len(f.objects[k]))+"</Size></Contents>")
	}
	io.WriteString(w, "</ListBucketResult>")
}

func newStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("AWS_CONFIG_FILE", "/nonexistent")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/nonexistent")
	srv := httptest.NewServer(&fakeS3{objects: map[string][]byte{}})
	t.Cleanup(srv.Close)
	s, err := New(context.Background(), Config{Bucket: "bkt", AWS: cloudaws.Config{
		Region: "us-east-1", Endpoint: srv.URL, AccessKeyID: "k", SecretAccessKey: "s",
	}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStore_RoundTrip(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	// A non-seekable reader of unknown size streams without buffering.
	if err := s.Put(ctx, bucket.PutInput{Key: "a.txt", Body: io.NopCloser(strings.NewReader("hello")), Size: -1, ContentType: "text/plain"}); err != nil {
		t.Fatal(err)
	}
	obj, body, err := s.Get(ctx, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	got, _ := io.ReadAll(body)
	if string(got) != "hello" || obj.LastModified.IsZero() {
		t.Fatalf("get: %q %+v", got, obj)
	}
	list, err := s.List(ctx, "")
	if err != nil || len(list) != 1 || list[0].Key != "a.txt" {
		t.Fatalf("list: %+v %v", list, err)
	}
	if err := s.Delete(ctx, "a.txt"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Get(ctx, "a.txt"); !errors.Is(err, bucket.ErrNotFound) {
		t.Fatalf("missing key: %v", err)
	}
}

func TestStore_PresignGet(t *testing.T) {
	url, err := newStore(t).PresignGet(context.Background(), "a.txt", time.Minute)
	if err != nil || !strings.Contains(url, "X-Amz-Signature") || !strings.Contains(url, "/bkt/a.txt") {
		t.Fatalf("presign: %s %v", url, err)
	}
}

func TestOpen_RegistersS3AndMinIO(t *testing.T) {
	t.Setenv("AWS_CONFIG_FILE", "/nonexistent")
	for _, p := range []bucket.Provider{bucket.ProviderS3, bucket.ProviderMinIO} {
		if _, err := bucket.Open(context.Background(), bucket.Config{Provider: p, Name: "b", Endpoint: "minio:9000"}); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
	}
}

func TestWithScheme(t *testing.T) {
	cases := map[[2]string]string{
		{"minio:9000", "false"}:         "http://minio:9000",
		{"minio:9000", "true"}:          "https://minio:9000",
		{"https://s3.example", "false"}: "https://s3.example",
		{"", "true"}:                    "",
	}
	for in, want := range cases {
		if got := withScheme(in[0], in[1] == "true"); got != want {
			t.Errorf("withScheme(%v)=%q, want %q", in, got, want)
		}
	}
}

func TestContract(t *testing.T) { buckettest.Run(t, newStore(t)) }

// Regression: PresignGet must refuse a non-positive ttl, one above the SigV4
// 7-day limit, and one above the configured cap.
func TestStore_PresignGet_RejectsTTLOutsideLimit(t *testing.T) {
	s := newStore(t)
	for _, ttl := range []time.Duration{0, -time.Second, bucket.MaxPresignTTL + time.Second} {
		if _, err := s.PresignGet(context.Background(), "a.txt", ttl); !errors.Is(err, bucket.ErrInvalidTTL) {
			t.Errorf("ttl %s: %v; want ErrInvalidTTL", ttl, err)
		}
	}
	s.presignMax = bucket.PresignLimit(time.Hour)
	if _, err := s.PresignGet(context.Background(), "a.txt", 2*time.Hour); !errors.Is(err, bucket.ErrInvalidTTL) {
		t.Errorf("above configured cap: %v; want ErrInvalidTTL", err)
	}
}

func TestOpen_MapsPresignMaxTTL(t *testing.T) {
	t.Setenv("AWS_CONFIG_FILE", "/nonexistent")
	st, err := bucket.Open(context.Background(), bucket.Config{Provider: bucket.ProviderS3, Name: "b", Endpoint: "minio:9000", PresignMaxTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if got := st.(*Store).presignMax; got != time.Hour {
		t.Errorf("presignMax=%s; want 1h", got)
	}
}
