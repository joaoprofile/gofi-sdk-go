package debug

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
)

func basicAuthMiddleware(user, pass string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		// Evaluate both comparisons so timing does not reveal which one failed.
		userOK := secureEqual(u, user)
		passOK := secureEqual(p, pass)
		if !ok || !userOK || !passOK {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("401 Unauthorized\n"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// secureEqual compares fixed-size digests so neither content nor length leaks.
func secureEqual(a, b string) bool {
	ha, hb := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}
