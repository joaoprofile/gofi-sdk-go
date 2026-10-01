package middleware

import (
	"net/http"
	"strings"
	"time"

	iamconfig "github.com/joaoprofile/gofi-sdk-go/iam/config"
	"github.com/joaoprofile/gofi-sdk-go/iam/port"
	"github.com/joaoprofile/gofi-sdk-go/iam/types"
)

// Cookie helpers for the refresh token and the IDP login state, driven by
// SecurityConfig. Every cookie is HttpOnly and Secure (unless CookieInsecure).

// SetRefreshCookie stores the session's refresh token in a cookie scoped to
// CookiePath (the refresh endpoint), expiring with the session.
func SetRefreshCookie(w http.ResponseWriter, sec iamconfig.SecurityConfig, s *types.Session) {
	sec.ApplyDefaults()
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure unless CookieInsecure (local http only)
		Name:     sec.CookieName,
		Value:    s.RefreshToken,
		Path:     sec.CookiePath,
		Domain:   sec.CookieDomain,
		Expires:  s.ExpiresAt,
		MaxAge:   max(int(time.Until(s.ExpiresAt).Seconds()), 1),
		HttpOnly: true,
		Secure:   !sec.CookieInsecure,
		SameSite: sec.CookieSameSite,
	})
}

// RefreshTokenFromCookie returns the refresh token cookie value, or "".
func RefreshTokenFromCookie(r *http.Request, sec iamconfig.SecurityConfig) string {
	sec.ApplyDefaults()
	c, err := r.Cookie(sec.CookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// ClearRefreshCookie deletes the refresh token cookie (logout).
func ClearRefreshCookie(w http.ResponseWriter, sec iamconfig.SecurityConfig) {
	sec.ApplyDefaults()
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- same attributes as the cookie it clears
		Name:     sec.CookieName,
		Path:     sec.CookiePath,
		Domain:   sec.CookieDomain,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   !sec.CookieInsecure,
		SameSite: sec.CookieSameSite,
	})
}

// idpStatePath scopes the IDP state cookie to the whole site: the callback
// route is the application's choice.
const idpStatePath = "/"

// SetIDPStateCookie keeps the flow's state and PKCE verifier for the callback,
// for IDPStateTTL. SameSite is Lax so it survives the IDP's redirect back.
func SetIDPStateCookie(w http.ResponseWriter, sec iamconfig.SecurityConfig, flow *port.IDPAuthURL) {
	sec.ApplyDefaults()
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure unless CookieInsecure (local http only)
		Name:     sec.IDPStateCookieName,
		Value:    flow.State + "." + flow.CodeVerifier, // both base64url: no "."
		Path:     idpStatePath,
		Domain:   sec.CookieDomain,
		MaxAge:   int(sec.IDPStateTTL.Seconds()),
		HttpOnly: true,
		Secure:   !sec.CookieInsecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// IDPStateFromCookie returns the state and PKCE verifier saved by
// SetIDPStateCookie; pass them as ExpectedState and CodeVerifier.
func IDPStateFromCookie(r *http.Request, sec iamconfig.SecurityConfig) (state, verifier string, ok bool) {
	sec.ApplyDefaults()
	c, err := r.Cookie(sec.IDPStateCookieName)
	if err != nil {
		return "", "", false
	}
	state, verifier, ok = strings.Cut(c.Value, ".")
	if !ok || state == "" || verifier == "" {
		return "", "", false
	}
	return state, verifier, true
}

// ClearIDPStateCookie deletes the IDP state cookie; call it in the callback
// so a state is never used twice.
func ClearIDPStateCookie(w http.ResponseWriter, sec iamconfig.SecurityConfig) {
	sec.ApplyDefaults()
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- same attributes as the cookie it clears
		Name:     sec.IDPStateCookieName,
		Path:     idpStatePath,
		Domain:   sec.CookieDomain,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   !sec.CookieInsecure,
		SameSite: http.SameSiteLaxMode,
	})
}
