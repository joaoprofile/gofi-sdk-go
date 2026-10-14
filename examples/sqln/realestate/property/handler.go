package property

import (
	"errors"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/joaoprofile/gofi-sdk-go/netx/httpx"
	"github.com/joaoprofile/gofi-sdk-go/sqln/connection"
)

// MaxQueryLength bounds the search sentence, in characters.
const MaxQueryLength = 200

// Handler serves the search and the listing API.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Handlers implements httpx.RouterHandler.
func (h *Handler) Handlers() []*httpx.Route {
	return httpx.PublicRoutes("/api",
		httpx.GET("/search").To(h.search),
		httpx.GET("/dictionary").To(h.dictionary),
		httpx.POST("/properties").To(h.create),
	)
}

// GET /api/search?q=casa nova 2 quartos batel&page=0
func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if utf8.RuneCountInString(q) > MaxQueryLength {
		httpx.Error(w, http.StatusBadRequest, errors.New("busca longa demais"))
		return
	}
	page, err := strconv.ParseUint(r.URL.Query().Get("page"), 10, 16)
	if err != nil {
		page = 0
	}
	resp, err := h.svc.Search(r.Context(), q, uint16(min(page, 1000)))
	if err != nil {
		// Database details stay in the logs; the client gets a generic error.
		connection.LogPostgresError(err)
		httpx.Error(w, http.StatusInternalServerError, errors.New("erro interno"))
		return
	}
	httpx.Response(w, http.StatusOK, resp)
}

// GET /api/dictionary — the codes and ids a listing refers to.
func (h *Handler) dictionary(w http.ResponseWriter, _ *http.Request) {
	httpx.Response(w, http.StatusOK, h.svc.Dictionary())
}

// POST /api/properties — a NewProperty; answers {"id": …}.
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var p NewProperty
	if err := httpx.ParseRequestBody(w, r, &p); err != nil {
		httpx.Error(w, http.StatusBadRequest, err)
		return
	}
	id, err := h.svc.Create(r.Context(), p)
	switch {
	case errors.Is(err, ErrInvalidProperty):
		httpx.Error(w, http.StatusBadRequest, err)
	case connection.IsForeignKeyViolation(err):
		httpx.Error(w, http.StatusBadRequest, errors.New("tipo, oferta, condição, anunciante, bairro ou característica inexistente"))
	case err != nil:
		connection.LogPostgresError(err)
		httpx.Error(w, http.StatusInternalServerError, errors.New("erro interno"))
	default:
		httpx.Response(w, http.StatusCreated, map[string]int64{"id": id})
	}
}
