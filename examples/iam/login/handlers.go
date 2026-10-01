package main

import (
	"errors"
	"net"
	"net/http"
	"time"

	iamconfig "github.com/joaoprofile/gofi-sdk-go/iam/config"
	"github.com/joaoprofile/gofi-sdk-go/iam/core"
	"github.com/joaoprofile/gofi-sdk-go/iam/middleware"
	"github.com/joaoprofile/gofi-sdk-go/iam/port"
	"github.com/joaoprofile/gofi-sdk-go/iam/types"
	"github.com/joaoprofile/gofi-sdk-go/netx"
)

var (
	errBadCredentials = errors.New("invalid email or password")
	errTooMany        = errors.New("too many attempts, try again later")
	errForbidden      = errors.New("forbidden")
)

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// tokenResponse is what token mode hands to the client.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"` // access token lifetime, in seconds
}

type handler struct {
	iam       *core.IAMService
	vault     *vault
	accessTTL time.Duration
	// cookies drives the iam cookie helpers: HttpOnly, Secure unless
	// CookieInsecure (local http), refresh cookie scoped to /auth/refresh.
	cookies iamconfig.SecurityConfig
}

func (h *handler) Handlers() []*netx.Route {
	public := netx.PublicRoutes("/auth",
		netx.POST("/token/login").To(h.tokenLogin),
		netx.POST("/token/refresh").To(h.tokenRefresh),
		netx.POST("/session/login").To(h.sessionLogin),
		netx.POST("/cookie/login").To(h.cookieLogin),
		netx.POST("/refresh").To(h.cookieRefresh),
	)
	private := netx.PrivateRoutes("/auth",
		netx.POST("/logout").To(h.logout),
		netx.POST("/logout-all").To(h.logoutAll),
	)
	api := netx.PrivateRoutes("/api",
		netx.GET("/me").To(h.me),
		netx.GET("/reports").To(h.reports),
		netx.GET("/sessions").To(h.sessions),
	)
	return append(append(public, private...), api...)
}

// login runs the two iam steps: Authenticate checks the password and lists
// the user's tenants; SelectTenant opens the session and issues the tokens.
func (h *handler) login(w http.ResponseWriter, r *http.Request) (*types.Session, error) {
	var in credentials
	if err := netx.ParseRequestBody(w, r, &in); err != nil {
		return nil, err
	}
	ip := clientIP(r)
	res, err := h.iam.Authenticate(r.Context(), port.AuthInput{Email: in.Email, Password: in.Password, IPAddress: ip})
	if err != nil {
		return nil, err
	}
	if len(res.Tenants) == 0 {
		return nil, core.ErrTenantAccessDenied
	}
	return h.iam.SelectTenant(r.Context(), port.SelectTenantInput{
		UserID:    res.UserID,
		TenantID:  res.Tenants[0].Tenant.ID,
		Module:    module,
		Ticket:    res.Ticket, // proves this Authenticate call; single-use
		IPAddress: ip,
		UserAgent: r.UserAgent(),
	})
}

// clientIP keys the per-IP login throttling. Behind a proxy, take it from the
// proxy's trusted header instead.
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// loginFailed answers without revealing whether the email exists: 429 while
// the throttler locks the email or IP out, 401 otherwise.
func loginFailed(w http.ResponseWriter, err error) {
	if errors.Is(err, core.ErrTooManyAttempts) {
		netx.Error(w, http.StatusTooManyRequests, errTooMany)
		return
	}
	netx.Error(w, http.StatusUnauthorized, errBadCredentials)
}

// POST /auth/token/login — token mode: the client keeps both tokens and sends
// "Authorization: Bearer <access_token>". Fits mobile apps, CLIs and APIs.
func (h *handler) tokenLogin(w http.ResponseWriter, r *http.Request) {
	s, err := h.login(w, r)
	if err != nil {
		loginFailed(w, err)
		return
	}
	netx.Response(w, http.StatusOK, h.toTokens(s, true))
}

// POST /auth/token/refresh — trades the refresh token for a new pair. The old
// pair stops working (rotation); reusing it revokes all the user's sessions.
func (h *handler) tokenRefresh(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := netx.ParseRequestBody(w, r, &in); err != nil {
		netx.Error(w, http.StatusBadRequest, err)
		return
	}
	s, err := h.iam.RefreshToken(r.Context(), in.RefreshToken)
	if err != nil {
		netx.Error(w, http.StatusUnauthorized, errUnauthenticated)
		return
	}
	netx.Response(w, http.StatusOK, h.toTokens(s, true))
}

// POST /auth/cookie/login — cookie mode: the access token goes in the body
// (kept in memory by the SPA), the refresh token in an HttpOnly cookie that
// the browser sends only to /auth/refresh.
func (h *handler) cookieLogin(w http.ResponseWriter, r *http.Request) {
	s, err := h.login(w, r)
	if err != nil {
		loginFailed(w, err)
		return
	}
	middleware.SetRefreshCookie(w, h.cookies, s)
	netx.Response(w, http.StatusOK, h.toTokens(s, false))
}

// POST /auth/refresh — cookie mode refresh: rotates the cookie's refresh token.
func (h *handler) cookieRefresh(w http.ResponseWriter, r *http.Request) {
	s, err := h.iam.RefreshToken(r.Context(), middleware.RefreshTokenFromCookie(r, h.cookies))
	if err != nil {
		middleware.ClearRefreshCookie(w, h.cookies)
		netx.Error(w, http.StatusUnauthorized, errUnauthenticated)
		return
	}
	middleware.SetRefreshCookie(w, h.cookies, s)
	netx.Response(w, http.StatusOK, h.toTokens(s, false))
}

// POST /auth/session/login — session mode: the tokens go to the vault and the
// browser gets only an HttpOnly cookie. Fits server-rendered apps and SPAs
// served by the same backend (BFF).
func (h *handler) sessionLogin(w http.ResponseWriter, r *http.Request) {
	s, err := h.login(w, r)
	if err != nil {
		loginFailed(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure is configurable only so the example runs over local http
		Name:     sessionCookie,
		Value:    h.vault.put(s),
		Path:     "/",
		Expires:  s.ExpiresAt,
		HttpOnly: true,                      // not readable from JavaScript
		Secure:   !h.cookies.CookieInsecure, // HTTPS only; off for local http
		SameSite: http.SameSiteLaxMode,      // not sent on cross-site POSTs
	})
	netx.Response(w, http.StatusOK, map[string]any{"user_id": s.UserID})
}

// POST /auth/logout — revokes the current session in any mode. Tokens
// already issued for it are rejected from now on, even before they expire.
func (h *handler) logout(w http.ResponseWriter, r *http.Request) {
	claims := claimsFrom(r.Context())
	if err := h.iam.Logout(r.Context(), claims.SessionID); err != nil {
		netx.Error(w, http.StatusInternalServerError, err)
		return
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		h.vault.delete(c.Value)
		http.SetCookie(w, &http.Cookie{ // #nosec G124 -- same attributes as the cookie it clears
			Name: sessionCookie, Path: "/", MaxAge: -1,
			HttpOnly: true, Secure: !h.cookies.CookieInsecure, SameSite: http.SameSiteLaxMode,
		})
	}
	middleware.ClearRefreshCookie(w, h.cookies)
	w.WriteHeader(http.StatusNoContent)
}

// POST /auth/logout-all — revokes every session of the user (all devices).
func (h *handler) logoutAll(w http.ResponseWriter, r *http.Request) {
	if err := h.iam.LogoutAll(r.Context(), claimsFrom(r.Context()).UserID); err != nil {
		netx.Error(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/me — who is calling, as seen by the handlers.
func (h *handler) me(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	netx.Response(w, http.StatusOK, map[string]any{
		"user_id":     c.UserID,
		"tenant_id":   c.TenantID,
		"roles":       c.Roles,
		"session_id":  c.SessionID,
		"expires_at":  c.ExpiresAt,
		"permissions": h.iam.RBAC().Permissions(*c),
	})
}

// GET /api/reports — needs reports:read (admin and viewer).
func (h *handler) reports(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(w, r, "reports", "read") {
		return
	}
	netx.Response(w, http.StatusOK, []map[string]any{{"month": "2026-08", "revenue": 12500}})
}

// GET /api/sessions — needs sessions:list (admin only): the user's active
// sessions, for a "logged-in devices" screen.
func (h *handler) sessions(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(w, r, "sessions", "list") {
		return
	}
	list, err := h.iam.ListSessions(r.Context(), claimsFrom(r.Context()).UserID)
	if err != nil {
		netx.Error(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, s := range list {
		out = append(out, map[string]any{"id": s.ID, "created_at": s.CreatedAt, "user_agent": s.UserAgent})
	}
	netx.Response(w, http.StatusOK, out)
}

// allowed checks a permission against the roles in the claims (RBAC).
func (h *handler) allowed(w http.ResponseWriter, r *http.Request, resource, action string) bool {
	if !h.iam.RBAC().Enforce(*claimsFrom(r.Context()), resource, action) {
		netx.Error(w, http.StatusForbidden, errForbidden)
		return false
	}
	return true
}

// toTokens builds the response; withRefresh is false in cookie mode, where
// the refresh token never reaches JavaScript.
func (h *handler) toTokens(s *types.Session, withRefresh bool) tokenResponse {
	out := tokenResponse{
		AccessToken: s.AccessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int(h.accessTTL.Seconds()),
	}
	if withRefresh {
		out.RefreshToken = s.RefreshToken
	}
	return out
}
