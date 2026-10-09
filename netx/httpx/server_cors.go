package httpx

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
)

const (
	headerOrigin = "Origin"
	headerVary   = "Vary"
	headerACRM   = "Access-Control-Request-Method"
	headerACRH   = "Access-Control-Request-Headers"
	headerACAO   = "Access-Control-Allow-Origin"
	headerACAC   = "Access-Control-Allow-Credentials"
	headerACAM   = "Access-Control-Allow-Methods"
	headerACAH   = "Access-Control-Allow-Headers"
	headerACEH   = "Access-Control-Expose-Headers"
	headerACMA   = "Access-Control-Max-Age"

	// AnyOrigin allows every origin; it requires AllowCredentials=false.
	AnyOrigin = "*"
)

// DefaultCORSConfig is the global policy; it allows no origin until
// AllowedOrigins is set.
func DefaultCORSConfig() CorsConfig {
	return CorsConfig{
		AllowedOrigins: []string{},
		AllowedMethods: []string{
			"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS",
		},
		AllowedHeaders:   DefaultCORSAllowedHeaders(),
		ExposeHeaders:    []string{"Content-Length", RequestIDHeader},
		AllowCredentials: true,
		MaxAge:           "86400",
	}
}

// DefaultCORSAllowedHeaders are the request headers a preflight may ask for
// when CorsConfig.AllowedHeaders is empty.
func DefaultCORSAllowedHeaders() []string {
	return []string{
		"Accept",
		"Accept-Language",
		"Content-Language",
		"Content-Type",
		"Authorization",
		"X-Requested-With",
		RequestIDHeader,
		"accepted-language",
		"Timezone",
	}
}

// Validate rejects origins that are not scheme://host[:port] (subdomain
// wildcards are not supported) and AnyOrigin combined with AllowCredentials.
func (c CorsConfig) Validate() error {
	var errs []error
	for _, o := range c.AllowedOrigins {
		if strings.TrimSpace(o) == AnyOrigin {
			if c.AllowCredentials {
				errs = append(errs, errors.New(`httpx: CORS origin "*" cannot be combined with AllowCredentials`))
			}
			continue
		}
		if _, ok := normalizeOrigin(o); !ok {
			errs = append(errs, fmt.Errorf("httpx: invalid CORS origin %q (want scheme://host[:port])", o))
		}
	}
	return errors.Join(errs...)
}

// CORSMiddleware applies config to every response and answers preflights.
// It panics on an invalid config (see CorsConfig.Validate).
func CORSMiddleware(config CorsConfig) Middleware {
	return corsMiddleware(mustCORSPolicy(config), nil)
}

// corsMiddleware applies the global policy; lookup, when set, returns the
// policy of a route with its own CORS for a preflight, nil otherwise.
func corsMiddleware(global *corsPolicy, lookup func(*http.Request) *corsPolicy) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isPreflight(r) {
				global.apply(w.Header(), r.Header.Get(headerOrigin))
				next.ServeHTTP(w, r)
				return
			}
			policy := global
			if lookup != nil {
				if p := lookup(r); p != nil {
					policy = p
				}
			}
			policy.preflight(w, r)
		})
	}
}

// routeCORS replaces the global CORS headers with the route policy.
func routeCORS(p *corsPolicy) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Del(headerACAO)
			h.Del(headerACAC)
			h.Del(headerACEH)
			p.apply(h, r.Header.Get(headerOrigin))
			next.ServeHTTP(w, r)
		})
	}
}

func isPreflight(r *http.Request) bool {
	return r.Method == http.MethodOptions && r.Header.Get(headerOrigin) != "" && r.Header.Get(headerACRM) != ""
}

// corsPolicy is a validated CorsConfig.
type corsPolicy struct {
	anyOrigin   bool
	origins     map[string]struct{} // normalized
	trusted     []string            // normalized exact origins, for CSRF
	credentials bool
	methods     map[string]struct{}
	methodList  string
	headers     map[string]string // lower-case name -> configured spelling
	expose      string
	maxAge      string
}

func newCORSPolicy(c CorsConfig) (*corsPolicy, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	p := &corsPolicy{
		origins:     map[string]struct{}{},
		credentials: c.AllowCredentials,
		methods:     map[string]struct{}{},
		headers:     map[string]string{},
		expose:      strings.Join(c.ExposeHeaders, ", "),
		maxAge:      c.MaxAge,
	}
	for _, o := range c.AllowedOrigins {
		if strings.TrimSpace(o) == AnyOrigin {
			p.anyOrigin = true
			continue
		}
		n, _ := normalizeOrigin(o)
		p.origins[n] = struct{}{}
		p.trusted = append(p.trusted, n)
	}
	p.setMethods(c.AllowedMethods)
	p.setHeaders(c.AllowedHeaders)
	return p, nil
}

func (p *corsPolicy) setMethods(methods []string) {
	if len(methods) == 0 {
		methods = DefaultCORSConfig().AllowedMethods
	}
	list := make([]string, 0, len(methods))
	for _, m := range methods {
		m = strings.ToUpper(strings.TrimSpace(m))
		if _, dup := p.methods[m]; m == "" || dup {
			continue
		}
		p.methods[m] = struct{}{}
		list = append(list, m)
	}
	p.methodList = strings.Join(list, ", ")
}

func (p *corsPolicy) setHeaders(headers []string) {
	if len(headers) == 0 {
		headers = DefaultCORSAllowedHeaders()
	}
	for _, h := range headers {
		if h = strings.TrimSpace(h); h != "" {
			p.headers[strings.ToLower(h)] = h
		}
	}
}

func mustCORSPolicy(c CorsConfig) *corsPolicy {
	p, err := newCORSPolicy(c)
	if err != nil {
		panic(err)
	}
	return p
}

// allowOrigin returns the Access-Control-Allow-Origin value for origin.
func (p *corsPolicy) allowOrigin(origin string) (string, bool) {
	if origin == "" {
		return "", false
	}
	if p.anyOrigin {
		return AnyOrigin, true
	}
	n, ok := normalizeOrigin(origin)
	if !ok {
		return "", false
	}
	if _, ok := p.origins[n]; !ok {
		return "", false
	}
	return origin, true
}

// apply sets the headers of an actual (non-preflight) response.
func (p *corsPolicy) apply(h http.Header, origin string) {
	addVary(h, headerOrigin)
	allow, ok := p.allowOrigin(origin)
	if !ok {
		return
	}
	h.Set(headerACAO, allow)
	if p.credentials {
		h.Set(headerACAC, "true")
	}
	if p.expose != "" {
		h.Set(headerACEH, p.expose)
	}
}

// preflight answers 204 with the policy, or 403 without CORS headers when
// the origin or the requested method is not allowed.
func (p *corsPolicy) preflight(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	addVary(h, headerOrigin)
	addVary(h, headerACRM)
	addVary(h, headerACRH)

	allow, ok := p.allowOrigin(r.Header.Get(headerOrigin))
	if _, method := p.methods[r.Header.Get(headerACRM)]; !ok || !method {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	h.Set(headerACAO, allow)
	if p.credentials {
		h.Set(headerACAC, "true")
	}
	h.Set(headerACAM, p.methodList)
	if allowed := p.allowHeaders(r.Header.Values(headerACRH)); allowed != "" {
		h.Set(headerACAH, allowed)
	}
	if p.maxAge != "" {
		h.Set(headerACMA, p.maxAge)
	}
	w.WriteHeader(http.StatusNoContent)
}

// allowHeaders is the intersection of the requested and allowed headers.
func (p *corsPolicy) allowHeaders(requested []string) string {
	var out []string
	for _, line := range requested {
		for name := range strings.SplitSeq(line, ",") {
			v, ok := p.headers[strings.ToLower(strings.TrimSpace(name))]
			if ok && !slices.Contains(out, v) {
				out = append(out, v)
			}
		}
	}
	return strings.Join(out, ", ")
}

func addVary(h http.Header, value string) {
	for _, line := range h.Values(headerVary) {
		for v := range strings.SplitSeq(line, ",") {
			if strings.EqualFold(strings.TrimSpace(v), value) {
				return
			}
		}
	}
	h.Add(headerVary, value)
}

// normalizeOrigin returns scheme://host[:port] in lower case, without the
// scheme's default port; ok is false for anything that is not an origin.
func normalizeOrigin(o string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(o))
	if err != nil || !isOriginShape(u) {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Host)
	if p := u.Port(); (scheme == "https" && p == "443") || (scheme == "http" && p == "80") {
		host = strings.TrimSuffix(host, ":"+p)
	}
	return scheme + "://" + host, true
}

func isOriginShape(u *url.URL) bool {
	return u.Scheme != "" && u.Host != "" && u.User == nil && u.Opaque == "" &&
		u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" &&
		(u.Path == "" || u.Path == "/") && !strings.Contains(u.Host, "*")
}

// routePreflights holds the policies of routes with their own CORS, by chi
// pattern and method; other methods on those paths keep the global policy.
type routePreflights struct {
	mux    *chi.Mux // patterns only, for matching
	global *corsPolicy
	paths  map[string]map[string]*corsPolicy
}

func newRoutePreflights(global *corsPolicy) *routePreflights {
	return &routePreflights{mux: chi.NewMux(), global: global, paths: map[string]map[string]*corsPolicy{}}
}

func (rp *routePreflights) add(pattern, method string, p *corsPolicy) {
	methods, ok := rp.paths[pattern]
	if !ok {
		methods = map[string]*corsPolicy{}
		rp.paths[pattern] = methods
		rp.mux.Method(http.MethodOptions, pattern, http.NotFoundHandler())
	}
	methods[method] = p
}

// lookup returns the preflight policy for r, or nil when its path has no
// route with its own CORS.
func (rp *routePreflights) lookup(r *http.Request) *corsPolicy {
	if len(rp.paths) == 0 {
		return nil
	}
	path := r.URL.RawPath
	if path == "" {
		path = r.URL.Path
	}
	methods, ok := rp.paths[rp.mux.Find(chi.NewRouteContext(), http.MethodOptions, path)]
	if !ok {
		return nil
	}
	if p, ok := methods[r.Header.Get(headerACRM)]; ok {
		return p
	}
	return rp.global
}

// crossOriginProtection trusts the given exact origins; malformed ones are
// skipped with a warning.
func crossOriginProtection(origins []string) *http.CrossOriginProtection {
	cop := http.NewCrossOriginProtection()
	for _, o := range origins {
		if err := cop.AddTrustedOrigin(o); err != nil {
			slog.Warn("httpx: origin not trusted by CrossOriginProtection", slog.String("origin", o), slog.Any("error", err))
		}
	}
	return cop
}
