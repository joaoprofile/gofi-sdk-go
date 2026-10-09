package handler

import (
	"net/http"

	"github.com/joaoprofile/gofi-sdk-go/netx/httpx"
)

// HealthHandler exposes a single public liveness probe.
type HealthHandler struct{}

func NewHealthHandler() *HealthHandler { return &HealthHandler{} }

// Handlers implements httpx.RouterHandler.
func (h *HealthHandler) Handlers() []*httpx.Route {
	return httpx.PublicRoutes("/health",
		httpx.GET("/").To(h.check),
	)
}

// GET /health
func (h *HealthHandler) check(w http.ResponseWriter, _ *http.Request) {
	// httpx.JSON writes an already-encoded payload as is.
	httpx.JSON(w, http.StatusOK, []byte(`{"status":"ok"}`))
}
