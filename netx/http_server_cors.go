package netx

import (
	"net/http"
	"strings"
)

func DefaultCORSConfig() CorsConfig {
	return CorsConfig{
		AllowedOrigins: []string{},
		AllowedMethods: []string{
			"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS",
		},
		AllowedHeaders: []string{
			"Content-Type",
			"Authorization",
			"X-Requested-With",
			"accepted-language",
			"Timezone",
		},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           "86400",
	}
}

func CORSMiddleware(config CorsConfig) Middleware {
	allowedOrigins := buildOriginMap(config.AllowedOrigins)
	allowedMethods := strings.Join(config.AllowedMethods, ", ")
	exposeHeaders := strings.Join(config.ExposeHeaders, ", ")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := normalizeOrigin(r.Header.Get("Origin"))

			addVaryHeaders(w)

			if isAllowedOrigin(origin, allowedOrigins) {
				applyOriginHeaders(w, origin, config.AllowCredentials)
			}

			applyCommonHeaders(w, r, allowedMethods, exposeHeaders, config.MaxAge)

			if r.Method == http.MethodOptions {
				handlePreflight(w, r, origin, allowedOrigins, allowedMethods, config.AllowCredentials)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func buildOriginMap(origins []string) map[string]struct{} {
	m := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		m[o] = struct{}{}
	}
	return m
}

func isAllowedOrigin(origin string, allowed map[string]struct{}) bool {
	_, ok := allowed[origin]
	return ok
}

func applyOriginHeaders(w http.ResponseWriter, origin string, allowCredentials bool) {
	w.Header().Set("Access-Control-Allow-Origin", origin)
	if allowCredentials {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
}

func applyCommonHeaders(
	w http.ResponseWriter,
	r *http.Request,
	allowedMethods string,
	exposeHeaders string,
	maxAge string,
) {
	w.Header().Set("Access-Control-Allow-Methods", allowedMethods)

	if reqHeaders := r.Header.Get("Access-Control-Request-Headers"); reqHeaders != "" {
		w.Header().Set("Access-Control-Allow-Headers", reqHeaders)
	}

	if exposeHeaders != "" {
		w.Header().Set("Access-Control-Expose-Headers", exposeHeaders)
	}

	if maxAge != "" {
		w.Header().Set("Access-Control-Max-Age", maxAge)
	}
}

func handlePreflight(
	w http.ResponseWriter,
	r *http.Request,
	origin string,
	allowedOrigins map[string]struct{},
	allowedMethods string,
	allowCredentials bool,
) {
	if origin != "" && isAllowedOrigin(origin, allowedOrigins) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		if allowCredentials {
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
	}

	w.Header().Set("Access-Control-Allow-Methods", allowedMethods)

	if reqHeaders := r.Header.Get("Access-Control-Request-Headers"); reqHeaders != "" {
		w.Header().Set("Access-Control-Allow-Headers", reqHeaders)
	}

	w.WriteHeader(http.StatusNoContent)
}

func addVaryHeaders(w http.ResponseWriter) {
	w.Header().Add("Vary", "Origin")
	w.Header().Add("Vary", "Access-Control-Request-Method")
	w.Header().Add("Vary", "Access-Control-Request-Headers")
}

func normalizeOrigin(o string) string {
	o = strings.TrimSpace(o)
	o = strings.TrimSuffix(o, "/")
	o = strings.Replace(o, ":443", "", 1)
	return o
}
