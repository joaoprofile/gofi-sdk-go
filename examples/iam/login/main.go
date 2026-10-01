// Command login shows the two ways to keep a user logged in with the iam
// package, with no database: users, sessions and tokens live in memory.
//
//	token mode    POST /auth/token/login    -> the client holds the JWT
//	session mode  POST /auth/session/login  -> the JWT stays on the server,
//	                                           the browser holds a cookie
package main

import (
	"context"
	"log"
	"os"

	"github.com/gofi-labs/gofi-sdk-go/gofi"
	"github.com/gofi-labs/gofi-sdk-go/gofi/component/httpserver"
	"github.com/gofi-labs/gofi-sdk-go/gofi/component/iam"
	iamconfig "github.com/gofi-labs/gofi-sdk-go/iam/config"
	"github.com/gofi-labs/gofi-sdk-go/iam/provider/rbac/roles"
	"github.com/gofi-labs/gofi-sdk-go/iam/types"
	"github.com/gofi-labs/gofi-sdk-go/obs/logging"
)

func main() {
	users, err := newDirectory()
	if err != nil {
		log.Fatalf("seed users: %v", err)
	}

	// 1. The iam service, built during Build from the environment: JWT_SECRET
	// (HS256 access tokens), ACCESS_TOKEN_TTL, REFRESH_TOKEN_TTL, JWT_ISSUER,
	// and sessions in memory (in Redis with CACHE_TYPE=redis). The
	// application supplies what the environment cannot: users, tenants, RBAC.
	identity := iam.New(iam.Config{
		User:   users,
		Tenant: users,
		RBAC: roles.NewRBACProvider(roles.Config{Permissions: roles.PermissionMap{
			"admin":  {"reports": {"*"}, "sessions": {"list"}},
			"viewer": {"reports": {"read"}},
		}}),
		OnEvent: logEvent,
	})

	// 2. HTTP server. netx rejects cross-site POSTs by Origin/Sec-Fetch-Site
	// by default: CSRF protection for the cookie mode.
	http := httpserver.New(":8080")

	// Build loads .env, sets up logging and starts iam before the server.
	svc, err := gofi.New("login-example").With(identity, http).Build()
	if err != nil {
		log.Fatal(err)
	}

	// 3. Routes need the running iam service, so they are added after Build;
	// the server only starts in ListenAndServe. UseAuth comes first: routes
	// pick up the auth middleware when added.
	v := newVault()
	http.UseAuth((&authenticator{iam: identity.Service(), vault: v}).Middleware).
		Handlers(&handler{
			iam:       identity.Service(),
			vault:     v,
			accessTTL: svc.Environment().Auth().AccessTokenTTL,
			cookies:   iamconfig.SecurityConfig{CookieInsecure: os.Getenv("COOKIE_SECURE") != "true"},
		})

	// Blocks until SIGINT/SIGTERM, then drains the server.
	if err := svc.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

// logEvent is the iam audit trail: logins, failures, refreshes, logouts and
// suspicious activity. Events never carry passwords or tokens.
func logEvent(ctx context.Context, e types.IAMEvent) {
	if e.Type == types.EventTokenValidated {
		return // fires on every authenticated request
	}
	args := []any{"type", e.Type, "user_id", e.UserID, "session_id", e.SessionID}
	if e.Error != nil {
		args = append(args, "error", e.Error)
	}
	if e.Error != nil || e.Type == types.EventSuspiciousActivity {
		logging.FromContext(ctx).WarnContext(ctx, "iam event", args...)
		return
	}
	logging.FromContext(ctx).InfoContext(ctx, "iam event", args...)
}
