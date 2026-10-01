package product

import (
	"errors"
	"net/http"

	"github.com/joaoprofile/gofi-sdk-go/netx"
	"github.com/joaoprofile/gofi-sdk-go/sqln"
	"github.com/joaoprofile/gofi-sdk-go/sqln/connection"
)

// Handler exposes the dynamic search over HTTP.
type Handler struct{ repo *Repository }

func NewHandler(repo *Repository) *Handler { return &Handler{repo: repo} }

// Handlers implements netx.RouterHandler.
func (h *Handler) Handlers() []*netx.Route {
	return netx.PublicRoutes("/products",
		netx.GET("/filters").To(h.fields),
		netx.POST("/search").To(h.search),
	)
}

// GET /products/filters — what can be filtered, so a frontend can build its filter screen.
func (h *Handler) fields(w http.ResponseWriter, _ *http.Request) {
	netx.Response(w, http.StatusOK, Fields)
}

// POST /products/search — the body is a sqln.Filters:
//
//	{"params": {"page": 0, "limit": 10, "sortField": "price", "sortDirection": "DESC"},
//	 "filters": [{"field": "name", "condition": "LIKE", "value": "pro"}]}
func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	var filters sqln.Filters
	if err := netx.ParseRequestBody(w, r, &filters); err != nil {
		netx.Error(w, http.StatusBadRequest, err)
		return
	}

	page, err := h.repo.Search(r.Context(), &filters)
	switch {
	case errors.Is(err, sqln.ErrInvalidFilter): // unknown field, operator, sort or limit
		netx.Error(w, http.StatusBadRequest, err)
	case err != nil:
		// Database details stay in the logs; the client gets a generic error.
		connection.LogPostgresError(err)
		netx.Error(w, http.StatusInternalServerError, errors.New("internal error"))
	default:
		netx.Response(w, http.StatusOK, page)
	}
}
