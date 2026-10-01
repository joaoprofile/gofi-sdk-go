package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	iamconfig "github.com/gofi-labs/gofi-sdk-go/iam/config"
	"github.com/gofi-labs/gofi-sdk-go/iam/port"
	"github.com/gofi-labs/gofi-sdk-go/iam/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func only(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	cs := rec.Result().Cookies()
	require.Len(t, cs, 1)
	return cs[0]
}

// The cookie settings in SecurityConfig used to be unused; the helpers apply
// them with secure defaults.
func TestRefreshCookie_SecureDefaults(t *testing.T) {
	rec := httptest.NewRecorder()
	s := &types.Session{RefreshToken: "sid.secret", ExpiresAt: time.Now().Add(time.Hour)}
	SetRefreshCookie(rec, iamconfig.SecurityConfig{}, s)

	c := only(t, rec)
	assert.Equal(t, "iam_rt", c.Name)
	assert.Equal(t, "sid.secret", c.Value)
	assert.Equal(t, "/auth/refresh", c.Path)
	assert.True(t, c.HttpOnly)
	assert.True(t, c.Secure)
	assert.Equal(t, http.SameSiteStrictMode, c.SameSite)
	assert.Positive(t, c.MaxAge)

	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	req.AddCookie(c)
	assert.Equal(t, "sid.secret", RefreshTokenFromCookie(req, iamconfig.SecurityConfig{}))
	assert.Empty(t, RefreshTokenFromCookie(httptest.NewRequest(http.MethodPost, "/", nil), iamconfig.SecurityConfig{}))

	rec = httptest.NewRecorder()
	ClearRefreshCookie(rec, iamconfig.SecurityConfig{})
	cleared := only(t, rec)
	assert.Equal(t, -1, cleared.MaxAge)
	assert.True(t, cleared.Secure && cleared.HttpOnly)
}

func TestRefreshCookie_ConfiguredAndInsecureDev(t *testing.T) {
	rec := httptest.NewRecorder()
	sec := iamconfig.SecurityConfig{CookieName: "rt", CookiePath: "/r", CookieDomain: "app.example", CookieInsecure: true, CookieSameSite: http.SameSiteLaxMode}
	SetRefreshCookie(rec, sec, &types.Session{RefreshToken: "x", ExpiresAt: time.Now().Add(time.Hour)})
	c := only(t, rec)
	assert.Equal(t, "rt", c.Name)
	assert.Equal(t, "/r", c.Path)
	assert.Equal(t, "app.example", c.Domain)
	assert.False(t, c.Secure)
	assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
}

func TestIDPStateCookie_RoundTrip(t *testing.T) {
	rec := httptest.NewRecorder()
	SetIDPStateCookie(rec, iamconfig.SecurityConfig{}, &port.IDPAuthURL{State: "st", CodeVerifier: "cv"})
	c := only(t, rec)
	assert.Equal(t, "iam_idp_state", c.Name)
	assert.True(t, c.HttpOnly && c.Secure)
	assert.Equal(t, http.SameSiteLaxMode, c.SameSite, "must survive the IDP redirect")
	assert.Equal(t, 600, c.MaxAge)

	req := httptest.NewRequest(http.MethodGet, "/cb", nil)
	req.AddCookie(c)
	state, verifier, ok := IDPStateFromCookie(req, iamconfig.SecurityConfig{})
	assert.True(t, ok)
	assert.Equal(t, "st", state)
	assert.Equal(t, "cv", verifier)

	for _, v := range []string{"", "noseparator", ".cv", "st."} {
		req := httptest.NewRequest(http.MethodGet, "/cb", nil)
		req.AddCookie(&http.Cookie{Name: "iam_idp_state", Value: v})
		_, _, ok := IDPStateFromCookie(req, iamconfig.SecurityConfig{})
		assert.False(t, ok, v)
	}
	_, _, ok = IDPStateFromCookie(httptest.NewRequest(http.MethodGet, "/cb", nil), iamconfig.SecurityConfig{})
	assert.False(t, ok)

	rec = httptest.NewRecorder()
	ClearIDPStateCookie(rec, iamconfig.SecurityConfig{})
	assert.Equal(t, -1, only(t, rec).MaxAge)
}
