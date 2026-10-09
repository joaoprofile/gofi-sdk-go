package httpx

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func csrfServer(cfg *WSConfig) *httpServer {
	ws := NewServer(cfg).(*httpServer)
	noContent := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }
	ws.AddHandlers(&plainRouterHandler{routes: PublicRoutes("/",
		POST("/pay").To(noContent),
		POST("/partner").To(noContent).Cors(&CorsConfig{AllowedOrigins: []string{"https://partner.test"}}),
	)})
	return ws
}

func csrfSend(ws *httpServer, path, origin, fetchSite string) int {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if fetchSite != "" {
		r.Header.Set("Sec-Fetch-Site", fetchSite)
	}
	w := httptest.NewRecorder()
	ws.router.ServeHTTP(w, r)
	return w.Code
}

// CSRF protection is on by default.
func TestCrossOriginProtection(t *testing.T) {
	ws := csrfServer(&WSConfig{AllowedOrigins: []string{"https://app.example.com"}})
	cases := []struct {
		name, path, origin, fetchSite string
		want                          int
	}{
		{"cross-site", "/pay", "https://evil.test", "cross-site", http.StatusForbidden},
		{"cross-site by Origin only", "/pay", "https://evil.test", "", http.StatusForbidden},
		{"trusted CORS origin", "/pay", "https://app.example.com", "cross-site", http.StatusNoContent},
		{"same-origin", "/pay", "https://api.example.com", "same-origin", http.StatusNoContent},
		{"same host by Origin", "/pay", "http://example.com", "", http.StatusNoContent},
		{"non-browser client", "/pay", "", "", http.StatusNoContent},
		{"route origin trusted on its route", "/partner", "https://partner.test", "cross-site", http.StatusNoContent},
		{"route origin not trusted elsewhere", "/pay", "https://partner.test", "cross-site", http.StatusForbidden},
		{"global origin not trusted on a route policy", "/partner", "https://app.example.com", "cross-site", http.StatusForbidden},
	}
	for _, tc := range cases {
		if got := csrfSend(ws, tc.path, tc.origin, tc.fetchSite); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestCrossOriginProtectionOptOut(t *testing.T) {
	ws := csrfServer(&WSConfig{DisableCrossOriginProtection: true})
	for _, path := range []string{"/pay", "/partner"} {
		if got := csrfSend(ws, path, "https://evil.test", "cross-site"); got != http.StatusNoContent {
			t.Errorf("%s with protection disabled: %d, want 204", path, got)
		}
	}
}

func TestH2C(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	ws := NewServer(&WSConfig{ServerPort: addr, H2C: true}).(*httpServer)
	proto := make(chan int, 1)
	ws.AddHandlers(&plainRouterHandler{routes: PublicRoutes("/", GET("/proto").To(func(w http.ResponseWriter, r *http.Request) {
		proto <- r.ProtoMajor
		w.WriteHeader(http.StatusNoContent)
	}))})
	go ws.ListenAndServe()
	defer ws.Shutdown(context.Background())

	var p http.Protocols
	p.SetUnencryptedHTTP2(true)
	client := &http.Client{Transport: &http.Transport{Protocols: &p}, Timeout: 5 * time.Second}
	var resp *http.Response
	for range 50 {
		if resp, err = client.Get("http://" + addr + "/proto"); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := <-proto; got != 2 {
		t.Errorf("ProtoMajor=%d, want 2", got)
	}
}

// Routes under the root prefix used to panic ("pay" without a slash).
func TestRootPrefixRoutes(t *testing.T) {
	for _, prefix := range []string{"", "/"} {
		ws := NewServer(&WSConfig{}).(*httpServer)
		ws.AddHandlers(&plainRouterHandler{routes: PublicRoutes(prefix, GET("/ping").To(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))})
		w := httptest.NewRecorder()
		ws.router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
		if w.Code != http.StatusNoContent {
			t.Errorf("prefix %q: status %d", prefix, w.Code)
		}
	}
}
