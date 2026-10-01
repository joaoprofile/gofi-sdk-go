package netx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// BenchmarkServerStack measures one GET through every default middleware.
func BenchmarkServerStack(b *testing.B) {
	ws := NewServer(&WSConfig{}).(*httpServer)
	ws.AddHandlers(&plainRouterHandler{routes: PublicRoutes("/api",
		GET("/items/{id}").To(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	)})
	h := ws.instrumented(ws.handler())
	req := httptest.NewRequest(http.MethodGet, "/api/items/42", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	b.ReportAllocs()
	for b.Loop() {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusNoContent {
			b.Fatalf("status %d", w.Code)
		}
	}
}
