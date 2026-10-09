// Package httpapi serves the people store over HTTP next to the gRPC API:
// JSON reads for browsers and tools, and the photo uploaded through gRPC as a
// plain image.
package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/joaoprofile/gofi-sdk-go/base/errs"
	"github.com/joaoprofile/gofi-sdk-go/examples/grpc/people"
	"github.com/joaoprofile/gofi-sdk-go/netx/httpx"
)

type Handler struct {
	store *people.Store
}

func NewHandler(store *people.Store) *Handler {
	return &Handler{store: store}
}

// Handlers implements httpx.RouterHandler.
func (h *Handler) Handlers() []*httpx.Route {
	return httpx.PublicRoutes("/people",
		httpx.GET("/").To(h.list),
		httpx.GET("/{id}").To(h.get),
		httpx.GET("/{id}/photo").To(h.photo),
	)
}

// GET /people?name=
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	httpx.Response(w, http.StatusOK, h.store.List(httpx.GetQueryParam("name", r)))
}

// GET /people/{id}
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(httpx.GetPathParam("id", r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	httpx.Response(w, http.StatusOK, p)
}

// GET /people/{id}/photo — open it in a browser after the client uploads it.
func (h *Handler) photo(w http.ResponseWriter, r *http.Request) {
	info, data, err := h.store.Photo(httpx.GetPathParam("id", r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", info.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
	w.Header().Set("ETag", `"`+info.SHA256+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func respondError(w http.ResponseWriter, r *http.Request, err error) {
	var appErr errs.AppError
	if !errors.As(err, &appErr) {
		appErr = errs.AppError{Kind: errs.KindOperation, Code: "INTERNAL", Message: "internal error", Err: err}
	}
	httpx.RespondError(w, r, appErr)
}
