package middleware

import (
	"net/http"

	"github.com/joaoprofile/gofi-sdk-go/netx"
	"github.com/joaoprofile/gofi-sdk-go/netx/httpx"
)

// APIVersion is a global middleware (registered with server.Use): it runs for
// every route, public or private, and stamps the API version and request ID
// on the response.
func APIVersion(version string) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-API-Version", version)
			w.Header().Set("X-Request-ID", netx.GetRequestID(r.Context()))
			next.ServeHTTP(w, r)
		})
	}
}
