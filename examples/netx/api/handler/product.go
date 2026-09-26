package handler

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joaoprofile/gofi/base/errs"
	"github.com/joaoprofile/gofi/netx"
)

var (
	errProductNotFound = errs.RegisterNotFound("PRODUCT_NOT_FOUND", "product %s not found")
	errInvalidID       = errs.RegisterValidation("INVALID_ID", "invalid id %q")
)

type Product struct {
	ID    int     `json:"id"`
	Name  string  `json:"name"`
	Price float64 `json:"price"`
	Stock int     `json:"stock"`
}

// ProductFilter is bound from the query string by netx.BindQueryParamsToStruct.
// e.g. GET /products?name=pen&min_stock=1
type ProductFilter struct {
	Name     string `form:"name"`
	MinStock int    `form:"min_stock"`
}

// ProductHandler shows every HTTP verb: reads are public, writes are private.
type ProductHandler struct {
	mu       sync.RWMutex
	nextID   int
	products map[int]Product
}

func NewProductHandler() *ProductHandler {
	return &ProductHandler{nextID: 1, products: map[int]Product{}}
}

// Handlers implements netx.RouterHandler. Public and private groups can share
// the same prefix; netx applies the auth middleware only to the private ones.
func (h *ProductHandler) Handlers() []*netx.Route {
	public := netx.PublicRoutes("/products",
		netx.GET("/").To(h.list),
		// Per-route CORS overrides the global policy for this route only.
		netx.GET("/{id}").To(h.get).Cors(&netx.CorsConfig{
			AllowedOrigins: []string{"*"},
			AllowedMethods: []string{http.MethodGet},
		}),
	)

	private := netx.PrivateRoutes("/products",
		netx.POST("/").To(h.create),
		// Per-route timeouts: extends read/write deadlines for this route only.
		netx.PUT("/{id}").To(h.replace).Timeouts(30*time.Second, 30*time.Second),
		netx.PATCH("/{id}").To(h.update),
		netx.DELETE("/{id}").To(h.delete),
	)

	return append(public, private...)
}

// GET /products?name=&min_stock=
func (h *ProductHandler) list(w http.ResponseWriter, r *http.Request) {
	var filter ProductFilter
	if err := netx.BindQueryParamsToStruct(r, w, &filter); err != nil {
		netx.Error(w, http.StatusBadRequest, err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	result := []Product{}
	for _, p := range h.products {
		if filter.Name != "" && !strings.Contains(strings.ToLower(p.Name), strings.ToLower(filter.Name)) {
			continue
		}
		if p.Stock < filter.MinStock {
			continue
		}
		result = append(result, p)
	}

	netx.Response(w, http.StatusOK, result)
}

// GET /products/{id}
func (h *ProductHandler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}

	h.mu.RLock()
	p, found := h.products[id]
	h.mu.RUnlock()

	if !found {
		netx.RespondError(w, r, errProductNotFound.New(strconv.Itoa(id)))
		return
	}

	netx.Response(w, http.StatusOK, p)
}

// POST /products
func (h *ProductHandler) create(w http.ResponseWriter, r *http.Request) {
	var p Product
	if err := netx.ParseRequestBody(w, r, &p); err != nil {
		netx.Error(w, http.StatusBadRequest, err)
		return
	}
	if !h.valid(w, p) {
		return
	}

	h.mu.Lock()
	p.ID = h.nextID
	h.nextID++
	h.products[p.ID] = p
	h.mu.Unlock()

	netx.Response(w, http.StatusCreated, p)
}

// PUT /products/{id} — replaces the whole resource.
func (h *ProductHandler) replace(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}

	var p Product
	if err := netx.ParseRequestBody(w, r, &p); err != nil {
		netx.Error(w, http.StatusBadRequest, err)
		return
	}
	if !h.valid(w, p) {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if _, found := h.products[id]; !found {
		netx.RespondError(w, r, errProductNotFound.New(strconv.Itoa(id)))
		return
	}

	p.ID = id
	h.products[id] = p
	netx.Response(w, http.StatusOK, p)
}

// PATCH /products/{id} — partial update. The body is decoded into pointers,
// so absent fields stay nil and are left untouched.
func (h *ProductHandler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}

	var patch struct {
		Name  *string  `json:"name"`
		Price *float64 `json:"price"`
		Stock *int     `json:"stock"`
	}
	if err := netx.ParseRequestBody(w, r, &patch); err != nil {
		netx.Error(w, http.StatusBadRequest, err)
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	p, found := h.products[id]
	if !found {
		netx.RespondError(w, r, errProductNotFound.New(strconv.Itoa(id)))
		return
	}
	if patch.Name != nil {
		p.Name = *patch.Name
	}
	if patch.Price != nil {
		p.Price = *patch.Price
	}
	if patch.Stock != nil {
		p.Stock = *patch.Stock
	}

	h.products[id] = p
	netx.Response(w, http.StatusOK, p)
}

// DELETE /products/{id}
func (h *ProductHandler) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if _, found := h.products[id]; !found {
		netx.RespondError(w, r, errProductNotFound.New(strconv.Itoa(id)))
		return
	}

	delete(h.products, id)
	// A nil payload writes only the status code.
	netx.JSON(w, http.StatusNoContent, nil)
}

// pathID reads the {id} path parameter, writing a 400 when it is not a number.
func (h *ProductHandler) pathID(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := netx.GetPathParam("id", r)
	id, err := strconv.Atoi(raw)
	if err != nil {
		netx.RespondError(w, r, errInvalidID.Wrap(err, raw))
		return 0, false
	}
	return id, true
}

// valid writes a 400 with per-field details when the product is invalid.
func (h *ProductHandler) valid(w http.ResponseWriter, p Product) bool {
	details := map[string]string{}
	if strings.TrimSpace(p.Name) == "" {
		details["name"] = "required"
	}
	if p.Price <= 0 {
		details["price"] = "must be greater than zero"
	}
	if len(details) > 0 {
		netx.ErrorDetails(w, http.StatusBadRequest, "invalid product", details)
		return false
	}
	return true
}
