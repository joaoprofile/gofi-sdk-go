package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/joaoprofile/gofi/netx"
)

// demoToken is hardcoded for the example only. A real service validates a JWT
// (see the iam package) or calls an identity provider.
const demoToken = "secret-token"

var errUnauthorized = errors.New("missing or invalid bearer token")

// Auth guards private routes. It is registered with server.UseAuth, so netx
// applies it only to routes declared through netx.PrivateRoutes.
func Auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token != demoToken {
			netx.Error(w, http.StatusUnauthorized, errUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
