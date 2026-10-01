package s3

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/joaoprofile/gofi-sdk-go/base/bucket"
	"github.com/joaoprofile/gofi-sdk-go/base/bucket/buckettest"
	cloudaws "github.com/joaoprofile/gofi-sdk-go/base/cloud/aws"
)

type fakeObject struct {
	data        []byte
	contentType string
	modified    time.Time
}

type fakeBucket struct {
	region  string
	created time.Time
	objects map[string]fakeObject
}

// fakeS3 is a minimal multi-bucket, path-style S3 endpoint. Like S3, it
// redirects requests signed for a region other than the bucket's.
type fakeS3 struct {
	mu       sync.Mutex
	buckets  map[string]*fakeBucket
	pageSize int // listing page size; small so pagination is exercised
	// deletes counts DeleteObjects requests.
	deletes int
	// maxBuckets is the max-buckets of the last ListBuckets request.
	maxBuckets string
	// failKey is reported as AccessDenied by DeleteObjects.
	failKey string
}

// newFakeS3 starts with the given buckets, in us-east-1.
func newFakeS3(names ...string) *fakeS3 {
	f := &fakeS3{buckets: map[string]*fakeBucket{}, pageSize: 2}
	for _, n := range names {
		f.buckets[n] = &fakeBucket{region: defaultRegion, created: time.Now(), objects: map[string]fakeObject{}}
	}
	return f
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	name, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	q := r.URL.Query()
	if name == "" {
		f.listBuckets(w, q)
		return
	}
	b := f.buckets[name]
	if key == "" {
		switch r.Method {
		case http.MethodPut:
			f.createBucket(w, r, name)
			return
		case http.MethodHead:
			switch {
			case b == nil:
				w.WriteHeader(http.StatusNotFound)
			case b.region != signingRegion(r):
				w.Header().Set("X-Amz-Bucket-Region", b.region)
				w.WriteHeader(http.StatusMovedPermanently)
			default:
				w.Header().Set("X-Amz-Bucket-Region", b.region)
			}
			return
		}
	}
	if b == nil {
		writeErr(w, r, http.StatusNotFound, "NoSuchBucket")
		return
	}
	if b.region != signingRegion(r) {
		w.Header().Set("X-Amz-Bucket-Region", b.region)
		writeErr(w, r, http.StatusMovedPermanently, "PermanentRedirect")
		return
	}
	switch {
	case key == "" && r.Method == http.MethodDelete:
		if len(b.objects) > 0 {
			writeErr(w, r, http.StatusConflict, "BucketNotEmpty")
			return
		}
		delete(f.buckets, name)
		w.WriteHeader(http.StatusNoContent)
	case key == "" && r.Method == http.MethodGet:
		f.list(w, b, q)
	case key == "" && r.Method == http.MethodPost && q.Has("delete"):
		f.deleteObjects(w, r, b)
	case r.Method == http.MethodPut:
		data, _ := io.ReadAll(r.Body)
		b.objects[key] = fakeObject{data: data, contentType: r.Header.Get("Content-Type"), modified: time.Now().UTC().Truncate(time.Second)}
		w.Header().Set("ETag", `"etag"`)
	case r.Method == http.MethodGet || r.Method == http.MethodHead:
		o, ok := b.objects[key]
		if !ok {
			writeErr(w, r, http.StatusNotFound, "NoSuchKey")
			return
		}
		w.Header().Set("Content-Type", o.contentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(o.data)))
		w.Header().Set("Last-Modified", o.modified.Format(http.TimeFormat))
		if r.Method == http.MethodGet {
			w.Write(o.data)
		}
	case r.Method == http.MethodDelete:
		delete(b.objects, key)
		w.WriteHeader(http.StatusNoContent)
	}
}

// signingRegion reads the region of the SigV4 credential scope.
func signingRegion(r *http.Request) string {
	_, cred, _ := strings.Cut(r.Header.Get("Authorization"), "Credential=")
	if parts := strings.Split(cred, "/"); len(parts) > 2 {
		return parts[2]
	}
	return ""
}

func writeErr(w http.ResponseWriter, r *http.Request, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		io.WriteString(w, "<Error><Code>"+code+"</Code><Message>"+code+"</Message></Error>")
	}
}

func esc(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

// listBuckets reports BucketRegion only when max-buckets is set, as S3 does.
func (f *fakeS3) listBuckets(w http.ResponseWriter, q url.Values) {
	f.maxBuckets = q.Get("max-buckets")
	names := make([]string, 0, len(f.buckets))
	for n := range f.buckets {
		names = append(names, n)
	}
	slices.Sort(names)
	io.WriteString(w, "<ListAllMyBucketsResult><Buckets>")
	for _, n := range names {
		b := f.buckets[n]
		io.WriteString(w, "<Bucket><Name>"+n+"</Name><CreationDate>"+b.created.UTC().Format(time.RFC3339)+"</CreationDate>")
		if f.maxBuckets != "" {
			io.WriteString(w, "<BucketRegion>"+b.region+"</BucketRegion>")
		}
		io.WriteString(w, "</Bucket>")
	}
	io.WriteString(w, "</Buckets></ListAllMyBucketsResult>")
}

// createBucket rejects an explicit us-east-1 constraint and one that differs
// from the signing region, like S3.
func (f *fakeS3) createBucket(w http.ResponseWriter, r *http.Request, name string) {
	if _, ok := f.buckets[name]; ok {
		writeErr(w, r, http.StatusConflict, "BucketAlreadyOwnedByYou")
		return
	}
	var body struct{ LocationConstraint string }
	xml.NewDecoder(r.Body).Decode(&body)
	region := body.LocationConstraint
	switch {
	case region == defaultRegion:
		writeErr(w, r, http.StatusBadRequest, "InvalidLocationConstraint")
		return
	case region == "":
		region = defaultRegion
	}
	if region != signingRegion(r) {
		writeErr(w, r, http.StatusBadRequest, "IllegalLocationConstraintException")
		return
	}
	f.buckets[name] = &fakeBucket{region: region, created: time.Now(), objects: map[string]fakeObject{}}
}

// list pages pageSize entries at a time; with a delimiter, keys below the
// next "/" collapse into CommonPrefixes.
func (f *fakeS3) list(w http.ResponseWriter, b *fakeBucket, q url.Values) {
	prefix, delim, token := q.Get("prefix"), q.Get("delimiter"), q.Get("continuation-token")
	type entry struct {
		name   string
		folder bool
	}
	seen := map[string]bool{}
	var entries []entry
	for k := range b.objects {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		e := entry{name: k}
		if i := strings.Index(k[len(prefix):], delim); delim != "" && i >= 0 {
			e = entry{name: k[:len(prefix)+i+len(delim)], folder: true}
		}
		if e.name > token && !seen[e.name] {
			seen[e.name] = true
			entries = append(entries, e)
		}
	}
	slices.SortFunc(entries, func(a, b entry) int { return strings.Compare(a.name, b.name) })
	truncated := len(entries) > f.pageSize
	if truncated {
		entries = entries[:f.pageSize]
	}
	w.Header().Set("Content-Type", "application/xml")
	io.WriteString(w, "<ListBucketResult><IsTruncated>"+strconv.FormatBool(truncated)+"</IsTruncated>")
	if truncated {
		io.WriteString(w, "<NextContinuationToken>"+esc(entries[len(entries)-1].name)+"</NextContinuationToken>")
	}
	for _, e := range entries {
		if e.folder {
			io.WriteString(w, "<CommonPrefixes><Prefix>"+esc(e.name)+"</Prefix></CommonPrefixes>")
			continue
		}
		io.WriteString(w, "<Contents><Key>"+esc(e.name)+"</Key><Size>"+strconv.Itoa(len(b.objects[e.name].data))+"</Size></Contents>")
	}
	io.WriteString(w, "</ListBucketResult>")
}

func (f *fakeS3) deleteObjects(w http.ResponseWriter, r *http.Request, b *fakeBucket) {
	f.deletes++
	var body struct {
		Object []struct{ Key string }
	}
	if err := xml.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, r, http.StatusBadRequest, "MalformedXML")
		return
	}
	io.WriteString(w, "<DeleteResult>")
	for _, o := range body.Object {
		if o.Key == f.failKey {
			io.WriteString(w, "<Error><Key>"+esc(o.Key)+"</Key><Code>AccessDenied</Code><Message>denied</Message></Error>")
			continue
		}
		delete(b.objects, o.Key)
	}
	io.WriteString(w, "</DeleteResult>")
}

func serve(t *testing.T, f *fakeS3) string {
	t.Helper()
	t.Setenv("AWS_CONFIG_FILE", "/nonexistent")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/nonexistent")
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return srv.URL
}

func testConfig(endpoint string) Config {
	return Config{Bucket: "bkt", AWS: cloudaws.Config{
		Region: defaultRegion, Endpoint: endpoint, AccessKeyID: "k", SecretAccessKey: "s",
	}}
}

// newTestStore opens the bucket "bkt" of a new fakeS3.
func newTestStore(t *testing.T) (*Store, *fakeS3) {
	t.Helper()
	f := newFakeS3("bkt")
	s, err := New(context.Background(), testConfig(serve(t, f)))
	if err != nil {
		t.Fatal(err)
	}
	return s, f
}

func newTestManager(t *testing.T, f *fakeS3) *Manager {
	t.Helper()
	m, err := NewManager(context.Background(), testConfig(serve(t, f)))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestStore_RoundTrip(t *testing.T) {
	s, _ := newTestStore(t)
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
	s, _ := newTestStore(t)
	url, err := s.PresignGet(context.Background(), "a.txt", time.Minute)
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

func TestOpen_RegistersManager(t *testing.T) {
	t.Setenv("AWS_CONFIG_FILE", "/nonexistent")
	for _, p := range []bucket.Provider{bucket.ProviderS3, bucket.ProviderMinIO} {
		m, err := bucket.OpenManager(context.Background(), bucket.Config{Provider: p, Endpoint: "minio:9000"})
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if mm, ok := m.(*Manager); !ok || mm.resolveRegion {
			t.Fatalf("%s: got %T; a custom endpoint must not resolve regions", p, m)
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

func TestContract(t *testing.T) {
	s, _ := newTestStore(t)
	buckettest.Run(t, s)
}

func TestManagerContract(t *testing.T) { buckettest.RunManager(t, newTestManager(t, newFakeS3())) }

// Regression: PresignGet must refuse a non-positive ttl, one above the SigV4
// 7-day limit, and one above the configured cap.
func TestStore_PresignGet_RejectsTTLOutsideLimit(t *testing.T) {
	s, _ := newTestStore(t)
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

func TestMapErr_Bucket(t *testing.T) {
	status := func(code int) error {
		return &awshttp.ResponseError{ResponseError: &smithyhttp.ResponseError{
			Response: &smithyhttp.Response{Response: &http.Response{StatusCode: code}},
			Err:      errors.New("http"),
		}}
	}
	api := func(code string) error { return &smithy.GenericAPIError{Code: code} }
	cases := []struct {
		err  error
		want error
	}{
		{api("NoSuchBucket"), bucket.ErrBucketNotFound},
		{api("NoSuchKey"), bucket.ErrNotFound},
		{api("NotFound"), bucket.ErrNotFound},
		{status(http.StatusNotFound), bucket.ErrNotFound},
		{api("BucketAlreadyExists"), bucket.ErrBucketExists},
		{api("BucketAlreadyOwnedByYou"), bucket.ErrBucketExists},
		{api("BucketNotEmpty"), bucket.ErrBucketNotEmpty},
		{api("InvalidBucketName"), bucket.ErrInvalidBucketName},
		{api("AccessDenied"), bucket.ErrAccessDenied},
		{api("AllAccessDisabled"), bucket.ErrAccessDenied},
		{status(http.StatusForbidden), bucket.ErrAccessDenied},
	}
	for _, c := range cases {
		got := mapErr(fmt.Errorf("op: %w", c.err))
		if !errors.Is(got, c.want) || !errors.Is(got, c.err) {
			t.Errorf("mapErr(%v)=%v; want %v keeping the cause", c.err, got, c.want)
		}
	}
	if got := mapErr(api("NoSuchKey")); errors.Is(got, bucket.ErrBucketNotFound) {
		t.Errorf("NoSuchKey must not be ErrBucketNotFound: %v", got)
	}
	if got := mapErr(api("SlowDown")); got.Error() != api("SlowDown").Error() {
		t.Errorf("unknown code changed: %v", got)
	}
}

func TestStore_MissingBucketAndKey(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	_, _, err := s.Get(ctx, "nope")
	if !errors.Is(err, bucket.ErrNotFound) || errors.Is(err, bucket.ErrBucketNotFound) {
		t.Errorf("missing key: %v; want ErrNotFound only", err)
	}
	s.bucket = "gone"
	_, _, err = s.Get(ctx, "nope")
	if !errors.Is(err, bucket.ErrBucketNotFound) || !errors.Is(err, bucket.ErrNotFound) {
		t.Errorf("missing bucket: %v; want ErrBucketNotFound", err)
	}
}

func TestDeleteMany_Batches(t *testing.T) {
	s, f := newTestStore(t)
	f.pageSize = 1000
	for i := range 2500 {
		f.buckets["bkt"].objects[fmt.Sprintf("x/%04d", i)] = fakeObject{}
	}
	n, err := bucket.DeletePrefix(context.Background(), s, "x/")
	if err != nil || n != 2500 {
		t.Fatalf("DeletePrefix=%d,%v; want 2500", n, err)
	}
	if f.deletes != 3 {
		t.Errorf("%d DeleteObjects requests; want 3", f.deletes)
	}
	if left := len(f.buckets["bkt"].objects); left != 0 {
		t.Errorf("%d objects left", left)
	}
}

func TestDeleteMany_ReportsKeyErrors(t *testing.T) {
	s, f := newTestStore(t)
	f.failKey = "b"
	err := s.DeleteMany(context.Background(), []string{"a", "b"})
	if !errors.Is(err, bucket.ErrAccessDenied) || !strings.Contains(err.Error(), `"b"`) {
		t.Fatalf("err=%v; want ErrAccessDenied naming the key", err)
	}
}

// plain hides the optional interfaces so bucket.ListDir takes its fallback.
type plain struct{ bucket.Store }

func TestListDir_MatchesFallback(t *testing.T) {
	s, f := newTestStore(t)
	for _, k := range []string{"a.txt", "docs/", "docs/b.txt", "docs/sub/c.txt", "docs/sub/d.txt", "e/f/g.txt"} {
		f.buckets["bkt"].objects[k] = fakeObject{}
	}
	ctx := context.Background()
	for _, prefix := range []string{"", "docs/", "docs/sub/", "none/"} {
		got, err := s.ListDir(ctx, prefix)
		if err != nil {
			t.Fatal(err)
		}
		want, err := bucket.ListDir(ctx, plain{s}, prefix)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ListDir(%q)=%+v; fallback %+v", prefix, got, want)
		}
	}
}

// A bucket in another region than the configured one opens in its own
// region: through the HeadBucket redirect and in ListBuckets.
func TestManager_OpensBucketInItsRegion(t *testing.T) {
	ctx := context.Background()
	f := newFakeS3()
	m := newTestManager(t, f)
	m.resolveRegion = true // as without a custom endpoint
	if err := m.CreateBucket(ctx, "far-bucket", bucket.CreateOptions{Region: "sa-east-1"}); err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}
	bs, err := m.ListBuckets(ctx)
	if err != nil || len(bs) != 1 || bs[0].Region != "sa-east-1" || f.maxBuckets != "1000" {
		t.Fatalf("ListBuckets=%+v,%v (max-buckets %q); want the region", bs, err, f.maxBuckets)
	}
	s, err := m.Open(ctx, "far-bucket")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Put(ctx, bucket.PutInput{Key: "k", Body: strings.NewReader("v"), Size: 1}); err != nil {
		t.Fatalf("Put in the bucket region: %v", err)
	}
	if _, rc, err := s.Get(ctx, "k"); err != nil {
		t.Fatalf("Get: %v", err)
	} else {
		rc.Close()
	}
	if err := m.DeleteBucket(ctx, "far-bucket", bucket.DeleteOptions{Force: true}); err != nil {
		t.Fatalf("DeleteBucket from the bucket region: %v", err)
	}

	// Without the lookup the configured region is used and S3 redirects.
	m.resolveRegion = false
	if err := m.CreateBucket(ctx, "far-bucket", bucket.CreateOptions{Region: "sa-east-1"}); err != nil {
		t.Fatal(err)
	}
	s, err = m.Open(ctx, "far-bucket")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, bucket.PutInput{Key: "k", Body: strings.NewReader("v"), Size: 1}); err == nil {
		t.Fatal("Put signed for the wrong region must fail")
	}
}

func TestManager_CreateBucketExistsInDefaultRegion(t *testing.T) {
	// us-east-1 answers a repeated CreateBucket with success; the Manager
	// must still report ErrBucketExists.
	m := newTestManager(t, newFakeS3("taken-bucket"))
	err := m.CreateBucket(context.Background(), "taken-bucket", bucket.CreateOptions{})
	if !errors.Is(err, bucket.ErrBucketExists) {
		t.Fatalf("err=%v; want ErrBucketExists", err)
	}
}
