package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/joaoprofile/gofi/iam/core"
	"github.com/joaoprofile/gofi/iam/types"
)

// contextKey is a private type to avoid context key collisions.
type contextKey string

const contextKeyClaims contextKey = "iam_claims"

// AuthMiddleware extracts and validates the Bearer token.
// On success, injects the validated claims into the context.
// On failure, responds with 401 and terminates the handler chain.
func AuthMiddleware(svc *core.IAMService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerToken(r)
			if token == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			claims, err := svc.ValidateToken(r.Context(), token)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), contextKeyClaims, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RBACMiddleware checks whether the claims authorize the given resource and action.
// Must be chained after AuthMiddleware. Responds with 403 on failure.
func RBACMiddleware(svc *core.IAMService, resource, action string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := ClaimsFromContext(r.Context())
			if claims == nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			if !svc.RBAC().Enforce(*claims, resource, action) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// TenantMiddleware verifies in the TenantPort whether the user still has access to the tenant in the claims.
// Ensures that access removal takes immediate effect without relying on token expiry.
// Must be chained after AuthMiddleware.
func TenantMiddleware(svc *core.IAMService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := ClaimsFromContext(r.Context())
			if claims == nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			if err := svc.Tenant().AssertAccess(r.Context(), claims.UserID, claims.TenantID, claims.Module); err != nil {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ClaimsFromContext extracts the claims injected by AuthMiddleware.
// Returns nil if not found, meaning the request did not pass through AuthMiddleware.
func ClaimsFromContext(ctx context.Context) *types.Claims {
	v, _ := ctx.Value(contextKeyClaims).(*types.Claims)
	return v
}

// extractBearerToken extracts the token from the Authorization: Bearer header.
func extractBearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
