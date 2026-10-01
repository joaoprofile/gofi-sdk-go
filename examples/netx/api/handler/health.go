package handler

import (
	"net/http"

	"github.com/gofi-labs/gofi-sdk-go/netx"
)

// HealthHandler exposes a single public liveness probe.
type HealthHandler struct{}

func NewHealthHandler() *HealthHandler { return &HealthHandler{} }

// Handlers implements netx.RouterHandler.
func (h *HealthHandler) Handlers() []*netx.Route {
	return netx.PublicRoutes("/health",
		netx.GET("/").To(h.check),
	)
}

// GET /health
func (h *HealthHandler) check(w http.ResponseWriter, _ *http.Request) {
	// netx.JSON writes an already-encoded payload as is.
	netx.JSON(w, http.StatusOK, []byte(`{"status":"ok"}`))
}
