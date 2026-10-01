package oci

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/base/bucket/buckettest"
)

// fakeOCI serves the Object Storage calls the Store makes: objects, paged
// listing, multipart uploads and pre-authenticated requests.
type fakeOCI struct {
	mu      sync.Mutex
	objects map[string][]byte
	types   map[string]string
	parts   map[string][]byte // uploadId -> assembled body
	uploads map[string]string // uploadId -> object
}

func newFakeOCI() *fakeOCI {
	return &fakeOCI{objects: map[string][]byte{}, types: map[string]string{}, parts: map[string][]byte{}, uploads: map[string]string{}}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]string{"code": "ObjectNotFound", "message": "not found"})
}

func (f *fakeOCI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	const base = "/n/ns/b/bkt/"
	path, ok := strings.CutPrefix(r.URL.Path, base)
	if !ok {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	switch {
	case path == "o" && r.Method == http.MethodGet:
		f.list(w, q.Get("prefix"), q.Get("start"))
	case strings.HasPrefix(path, "o/"):
		f.object(w, r, strings.TrimPrefix(path, "o/"))
	case path == "u" && r.Method == http.MethodPost:
		var body struct{ Object string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		id := "up-" + strconv.Itoa(len(f.uploads)+1)
		f.uploads[id] = body.Object
		writeJSON(w, http.StatusOK, map[string]any{"uploadId": id, "namespace": "ns", "bucket": "bkt", "object": body.Object, "timeCreated": time.Now()})
	case strings.HasPrefix(path, "u/"):
		id := q.Get("uploadId")
		switch r.Method {
		case http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			f.parts[id] = append(f.parts[id], b...)
			w.Header().Set("ETag", "part-etag")
		case http.MethodPost:
			obj := f.uploads[id]
			f.objects[obj] = f.parts[id]
			w.Header().Set("ETag", "etag")
		case http.MethodDelete:
			delete(f.parts, id)
			w.WriteHeader(http.StatusNoContent)
		}
	case (path == "p" || path == "p/") && r.Method == http.MethodPost:
		var body struct{ ObjectName string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "par", "name": "dl", "accessUri": "/p/token/n/ns/b/bkt/o/" + body.ObjectName,
			"accessType": "ObjectRead", "timeCreated": time.Now(), "timeExpires": time.Now().Add(time.Hour),
		})
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeOCI) object(w http.ResponseWriter, r *http.Request, name string) {
	switch r.Method {
	case http.MethodPut:
		b, _ := io.ReadAll(r.Body)
		f.objects[name] = b
		f.types[name] = r.Header.Get("Content-Type")
		w.Header().Set("ETag", "etag")
	case http.MethodGet:
		b, ok := f.objects[name]
		if !ok {
			notFound(w)
			return
		}
		w.Header().Set("Content-Type", f.types[name])
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		_, _ = w.Write(b)
	case http.MethodDelete:
		if _, ok := f.objects[name]; !ok {
			notFound(w)
			return
		}
		delete(f.objects, name)
		w.WriteHeader(http.StatusNoContent)
	}
}

// list pages two objects at a time.
func (f *fakeOCI) list(w http.ResponseWriter, prefix, start string) {
	names := make([]string, 0, len(f.objects))
	for k := range f.objects {
		if strings.HasPrefix(k, prefix) && k >= start {
			names = append(names, k)
		}
	}
	slices.Sort(names)
	resp := map[string]any{}
	if len(names) > 2 {
		resp["nextStartWith"] = names[2]
		names = names[:2]
	}
	objs := make([]map[string]any, 0, len(names))
	for _, n := range names {
		objs = append(objs, map[string]any{"name": n, "size": len(f.objects[n])})
	}
	resp["objects"] = objs
	writeJSON(w, http.StatusOK, resp)
}

func TestContract(t *testing.T) {
	srv := httptest.NewServer(newFakeOCI())
	t.Cleanup(srv.Close)
	cfg := validConfig()
	cfg.Bucket = "bkt"
	cfg.Namespace = "ns"
	cfg.Endpoint = srv.URL
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	buckettest.Run(t, s)
}
