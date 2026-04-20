package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/joaoprofile/gofi/iam/core"
	"github.com/joaoprofile/gofi/iam/port"
	"github.com/joaoprofile/gofi/iam/provider/memory"
	"github.com/joaoprofile/gofi/iam/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAuthPort is a test double for port.AuthPort.
type stubAuthPort struct {
	claims      *types.Claims
	validateErr error
}

func (s *stubAuthPort) Authenticate(_ context.Context, _ port.AuthInput) (*port.AuthResult, error) {
	return nil, nil
}
func (s *stubAuthPort) SelectTenant(_ context.Context, _ port.SelectTenantInput) (*types.Session, error) {
	return nil, nil
}
func (s *stubAuthPort) Logout(_ context.Context, _ string) error { return nil }
func (s *stubAuthPort) RefreshToken(_ context.Context, _ string) (*types.Session, error) {
	return nil, nil
}
func (s *stubAuthPort) ValidateToken(_ context.Context, _ string) (*types.Claims, error) {
	return s.claims, s.validateErr
}

// stubTenantPort is a test double for port.TenantPort.
type stubTenantPort struct {
	accessErr error
}

func (s *stubTenantPort) ListUserTenants(_ context.Context, _ string) ([]types.TenantAccess, error) {
	return nil, nil
}
func (s *stubTenantPort) AssertAccess(_ context.Context, _, _, _ string) error {
	return s.accessErr
}

// stubRBACPort is a test double for port.RBACPort.
type stubRBACPort struct {
	enforce bool
}

func (s *stubRBACPort) Enforce(_ types.Claims, _, _ string) bool     { return s.enforce }
func (s *stubRBACPort) Permissions(_ types.Claims) []port.Permission { return nil }

func buildService(auth port.AuthPort, tenant port.TenantPort, rbac port.RBACPort) *core.IAMService {
	return core.NewService(core.ServiceConfig{
		Auth:    auth,
		Tenant:  tenant,
		RBAC:    rbac,
		Session: memory.NewTestProvider(),
		IDPs:    make(map[string]*core.IDPService),
	})
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestAuthMiddleware_Success(t *testing.T) {
	claims := &types.Claims{UserID: "u1", TenantID: "t1", ExpiresAt: time.Now().Add(time.Hour)}
	auth := &stubAuthPort{claims: claims}
	svc := buildService(auth, &stubTenantPort{}, &stubRBACPort{})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	var capturedClaims *types.Claims
	handler := AuthMiddleware(svc)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		capturedClaims = ClaimsFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, capturedClaims)
	assert.Equal(t, "u1", capturedClaims.UserID)
}

func TestAuthMiddleware_MissingToken(t *testing.T) {
	svc := buildService(&stubAuthPort{}, &stubTenantPort{}, &stubRBACPort{})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	AuthMiddleware(svc)(okHandler()).ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	auth := &stubAuthPort{validateErr: core.ErrTokenInvalid}
	svc := buildService(auth, &stubTenantPort{}, &stubRBACPort{})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()

	AuthMiddleware(svc)(okHandler()).ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_BearerCaseInsensitive(t *testing.T) {
	claims := &types.Claims{UserID: "u1"}
	auth := &stubAuthPort{claims: claims}
	svc := buildService(auth, &stubTenantPort{}, &stubRBACPort{})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "BEARER valid-token")
	w := httptest.NewRecorder()

	AuthMiddleware(svc)(okHandler()).ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRBACMiddleware_Allowed(t *testing.T) {
	claims := &types.Claims{UserID: "u1", Roles: []string{"admin"}}
	auth := &stubAuthPort{claims: claims}
	rbac := &stubRBACPort{enforce: true}
	svc := buildService(auth, &stubTenantPort{}, rbac)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyClaims, claims))
	w := httptest.NewRecorder()

	RBACMiddleware(svc, "orders", "read")(okHandler()).ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRBACMiddleware_Forbidden(t *testing.T) {
	claims := &types.Claims{UserID: "u1", Roles: []string{"viewer"}}
	rbac := &stubRBACPort{enforce: false}
	svc := buildService(&stubAuthPort{}, &stubTenantPort{}, rbac)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyClaims, claims))
	w := httptest.NewRecorder()

	RBACMiddleware(svc, "orders", "delete")(okHandler()).ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestRBACMiddleware_NoClaims(t *testing.T) {
	svc := buildService(&stubAuthPort{}, &stubTenantPort{}, &stubRBACPort{enforce: true})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	RBACMiddleware(svc, "orders", "read")(okHandler()).ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestTenantMiddleware_Allowed(t *testing.T) {
	claims := &types.Claims{UserID: "u1", TenantID: "t1"}
	svc := buildService(&stubAuthPort{}, &stubTenantPort{}, &stubRBACPort{})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyClaims, claims))
	w := httptest.NewRecorder()

	TenantMiddleware(svc)(okHandler()).ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestTenantMiddleware_AccessDenied(t *testing.T) {
	claims := &types.Claims{UserID: "u1", TenantID: "t1"}
	tenant := &stubTenantPort{accessErr: core.ErrTenantAccessDenied}
	svc := buildService(&stubAuthPort{}, tenant, &stubRBACPort{})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyClaims, claims))
	w := httptest.NewRecorder()

	TenantMiddleware(svc)(okHandler()).ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestTenantMiddleware_NoClaims(t *testing.T) {
	svc := buildService(&stubAuthPort{}, &stubTenantPort{}, &stubRBACPort{})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	TenantMiddleware(svc)(okHandler()).ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestClaimsFromContext_Present(t *testing.T) {
	claims := &types.Claims{UserID: "u1"}
	ctx := context.WithValue(context.Background(), contextKeyClaims, claims)
	result := ClaimsFromContext(ctx)
	assert.Equal(t, claims, result)
}

func TestClaimsFromContext_Missing(t *testing.T) {
	result := ClaimsFromContext(context.Background())
	assert.Nil(t, result)
}

func TestExtractBearerToken_Valid(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer my-token")
	assert.Equal(t, "my-token", extractBearerToken(req))
}

func TestExtractBearerToken_MissingHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	assert.Equal(t, "", extractBearerToken(req))
}

func TestExtractBearerToken_WrongScheme(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	assert.Equal(t, "", extractBearerToken(req))
}

func TestExtractBearerToken_MalformedHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "onlyone")
	assert.Equal(t, "", extractBearerToken(req))
}

func TestExtractBearerToken_CaseInsensitiveBearer(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "bearer token-value")
	assert.Equal(t, "token-value", extractBearerToken(req))
}

func TestExtractBearerToken_TrimsSpaces(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer   spaced-token  ")
	assert.Equal(t, "spaced-token", extractBearerToken(req))
}

func TestAuthMiddleware_ChainInjectsClaimsForNextHandler(t *testing.T) {
	claims := &types.Claims{UserID: "u1", TenantID: "t1"}
	auth := &stubAuthPort{claims: claims}
	svc := buildService(auth, &stubTenantPort{}, &stubRBACPort{})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer tok")
	w := httptest.NewRecorder()

	var received *types.Claims
	AuthMiddleware(svc)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		received = ClaimsFromContext(r.Context())
	})).ServeHTTP(w, req)

	require.NotNil(t, received)
	assert.Equal(t, "u1", received.UserID)
	assert.Equal(t, "t1", received.TenantID)
}
