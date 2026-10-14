// Package web serves the search page. Script and style are separate files:
// the server's Content-Security-Policy refuses inline ones.
package web

import (
	"embed"
	"net/http"

	"github.com/joaoprofile/gofi-sdk-go/netx/httpx"
)

//go:embed index.html app.js app.css favicon.svg
var files embed.FS

// Handler serves the page and its assets.
type Handler struct{}

// Handlers implements httpx.RouterHandler.
func (Handler) Handlers() []*httpx.Route {
	return httpx.PublicRoutes("",
		httpx.GET("/").To(serve("index.html", "text/html; charset=utf-8")),
		httpx.GET("/app.js").To(serve("app.js", "text/javascript; charset=utf-8")),
		httpx.GET("/app.css").To(serve("app.css", "text/css; charset=utf-8")),
		httpx.GET("/favicon.svg").To(serve("favicon.svg", "image/svg+xml")),
	)
}

func serve(name, contentType string) func(http.ResponseWriter, *http.Request) {
	body, err := files.ReadFile(name)
	if err != nil {
		panic(err) // embedded at build time
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(body)
	}
}
