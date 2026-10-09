package handler

import (
	"net/http"
	"sync"

	"github.com/joaoprofile/gofi-sdk-go/netx/httpx"
)

type Order struct {
	ID        int    `json:"id"`
	ProductID int    `json:"product_id"`
	Quantity  int    `json:"quantity"`
	Status    string `json:"status"`
}

// OrderHandler is fully private: every route requires the auth middleware.
type OrderHandler struct {
	mu     sync.RWMutex
	nextID int
	orders []Order
}

func NewOrderHandler() *OrderHandler {
	return &OrderHandler{nextID: 1}
}

// Handlers implements httpx.RouterHandler.
func (h *OrderHandler) Handlers() []*httpx.Route {
	return httpx.PrivateRoutes("/orders",
		httpx.GET("/").To(h.list),
		httpx.POST("/").To(h.create),
	)
}

// GET /orders?status=pending
func (h *OrderHandler) list(w http.ResponseWriter, r *http.Request) {
	// GetQueryParam returns a single value, already lowercased.
	status := httpx.GetQueryParam("status", r)

	h.mu.RLock()
	defer h.mu.RUnlock()

	result := []Order{}
	for _, o := range h.orders {
		if status == "" || o.Status == status {
			result = append(result, o)
		}
	}

	httpx.Response(w, http.StatusOK, result)
}

// POST /orders
func (h *OrderHandler) create(w http.ResponseWriter, r *http.Request) {
	var o Order
	if err := httpx.ParseRequestBody(w, r, &o); err != nil {
		httpx.Error(w, http.StatusBadRequest, err)
		return
	}
	if o.ProductID <= 0 || o.Quantity <= 0 {
		httpx.ErrorDetails(w, http.StatusBadRequest, "invalid order", map[string]string{
			"product_id": "required",
			"quantity":   "must be greater than zero",
		})
		return
	}

	h.mu.Lock()
	o.ID = h.nextID
	o.Status = "pending"
	h.nextID++
	h.orders = append(h.orders, o)
	h.mu.Unlock()

	httpx.Response(w, http.StatusCreated, o)
}
