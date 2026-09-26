package handler

import (
	"net/http"
	"sync"

	"github.com/joaoprofile/gofi/netx"
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

// Handlers implements netx.RouterHandler.
func (h *OrderHandler) Handlers() []*netx.Route {
	return netx.PrivateRoutes("/orders",
		netx.GET("/").To(h.list),
		netx.POST("/").To(h.create),
	)
}

// GET /orders?status=pending
func (h *OrderHandler) list(w http.ResponseWriter, r *http.Request) {
	// GetQueryParam returns a single value, already lowercased.
	status := netx.GetQueryParam("status", r)

	h.mu.RLock()
	defer h.mu.RUnlock()

	result := []Order{}
	for _, o := range h.orders {
		if status == "" || o.Status == status {
			result = append(result, o)
		}
	}

	netx.Response(w, http.StatusOK, result)
}

// POST /orders
func (h *OrderHandler) create(w http.ResponseWriter, r *http.Request) {
	var o Order
	if err := netx.ParseRequestBody(w, r, &o); err != nil {
		netx.Error(w, http.StatusBadRequest, err)
		return
	}
	if o.ProductID <= 0 || o.Quantity <= 0 {
		netx.ErrorDetails(w, http.StatusBadRequest, "invalid order", map[string]string{
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

	netx.Response(w, http.StatusCreated, o)
}
