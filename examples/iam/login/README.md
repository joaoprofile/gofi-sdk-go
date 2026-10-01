# iam — login example

Login with the `iam` package in its three usual shapes, with **no database**:
users, sessions and tokens live in memory.

| Mode | Login | The client keeps | Sends on each request | Fits |
|------|-------|------------------|-----------------------|------|
| **Token** | `POST /auth/token/login` | access JWT + refresh token | `Authorization: Bearer <jwt>` | mobile apps, CLIs, service-to-service |
| **Cookie** | `POST /auth/cookie/login` | access JWT in memory; refresh token in an HttpOnly cookie | `Authorization: Bearer <jwt>` | SPAs calling their own API |
| **Session** | `POST /auth/session/login` | an opaque `sid` cookie (HttpOnly) | the cookie, automatically | web apps (BFF): the JWT **stays on the backend** |

In every mode each JWT is tied to a server-side session, so **logout is
immediate**: a revoked session rejects its tokens even before they expire.
Repeated wrong passwords lock the email (and the client IP) out for a while:
the login answers `429` until the lockout ends.

```
login/
├── .env         # APP_ENVIRONMENT, JWT_SECRET, ACCESS_TOKEN_TTL, COOKIE_SECURE
├── main.go      # gofi.New with the iam and httpserver components
├── users.go     # in-memory users and tenant: port.UserPort + port.TenantPort
├── auth.go      # middleware: Bearer JWT or session cookie; token vault
└── handlers.go  # login (3 modes), refresh, logout, /api/me, /api/reports, /api/sessions
```

## Run

```sh
cd examples/iam/login
go run .
```

| User | Password | Role | Can do |
|------|----------|------|--------|
| `admin@example.com` | `admin123` | admin | `reports:*`, `sessions:list` |
| `viewer@example.com` | `viewer123` | viewer | `reports:read` |

Configuration comes from [.env](.env), read automatically outside
`prod`/`stage`; variables set in the shell win.

| Variable | Use |
|----------|-----|
| `JWT_SECRET` | signs the access tokens (32+ bytes); required |
| `ACCESS_TOKEN_TTL` | access token lifetime (default 15m); `ACCESS_TOKEN_TTL=20s go run .` shows the refresh |
| `REFRESH_TOKEN_TTL`, `JWT_ISSUER` | session lifetime (default 7 days) and token issuer |
| `CACHE_TYPE=redis`, `CACHE_*` | keep sessions, login throttling and single-use tickets in Redis instead of memory (several instances; required in `stage`/`prod`) |
| `IAM_LOGIN_MAX_ATTEMPTS`, `IAM_LOGIN_LOCKOUT` | failed logins per email before a lockout (default 5; 4x per IP) and its duration (default 15m) |
| `COOKIE_SECURE` | `true` behind HTTPS |

## Token mode

```sh
JSON='Content-Type: application/json'   # netx rejects other bodies with 415

curl -s -X POST localhost:8080/auth/token/login -H "$JSON" \
  -d '{"email":"admin@example.com","password":"admin123"}'
# {"access_token":"eyJ...","refresh_token":"...","token_type":"Bearer","expires_in":900}

AT='<access_token>'; RT='<refresh_token>'

curl localhost:8080/api/me       -H "Authorization: Bearer $AT"
curl localhost:8080/api/reports  -H "Authorization: Bearer $AT"
curl localhost:8080/api/sessions -H "Authorization: Bearer $AT"   # 403 for viewer

# New pair when the access token expires. The old pair stops working.
curl -X POST localhost:8080/auth/token/refresh -H "$JSON" -d "{\"refresh_token\":\"$RT\"}"

curl -X POST localhost:8080/auth/logout     -H "Authorization: Bearer $AT"   # this device
curl -X POST localhost:8080/auth/logout-all -H "Authorization: Bearer $AT"   # all devices
```

Refresh tokens rotate: each one works **once**. Presenting a used one again
is treated as theft (`security.suspicious` event) and revokes every session
of the user. Each refresh also re-checks that the user is active and still
has the tenant, and no session outlives `SessionMaxLifetime` (default 30
days) from the original login.

## Cookie mode (refresh token in a cookie)

```sh
curl -X POST localhost:8080/auth/cookie/login -c jar.txt -H "$JSON" \
  -d '{"email":"admin@example.com","password":"admin123"}'
# {"access_token":"eyJ...","token_type":"Bearer","expires_in":900}
# Set-Cookie: iam_rt=...; Path=/auth/refresh; HttpOnly; SameSite=Strict

curl -X POST localhost:8080/auth/refresh -b jar.txt -c jar.txt   # new access token, rotated cookie
```

The cookie helpers of `iam/middleware` (`SetRefreshCookie`,
`RefreshTokenFromCookie`, `ClearRefreshCookie`, and the IDP state trio) apply
`SecurityConfig`'s cookie settings: HttpOnly, Secure unless `CookieInsecure`
(set here from `COOKIE_SECURE` for local http), and a path limited to the
refresh endpoint, so the refresh token is sent nowhere else.

## Session mode (token on the backend)

```sh
curl -X POST localhost:8080/auth/session/login -c jar.txt -H "$JSON" \
  -d '{"email":"viewer@example.com","password":"viewer123"}'
# Set-Cookie: sid=...; HttpOnly; SameSite=Lax

curl localhost:8080/api/me -b jar.txt
curl -X POST localhost:8080/auth/logout -b jar.txt -c jar.txt
```

The login still issues a JWT and a refresh token, but they go to a vault on
the server ([auth.go](auth.go)); the browser gets only a random id.
On each request the middleware looks the tokens up and validates the JWT.
When it has expired, the middleware renews it with the stored refresh token,
so the user stays logged in for the whole session (7 days) without noticing.

Why this shape for browsers: JavaScript never touches a token, so an XSS
cannot steal one. The cookie is `HttpOnly` and `SameSite=Lax`, and the server
enables `netx` `CrossOriginProtection`, which rejects cross-site POSTs (CSRF).

## How the pieces fit

0. **Service** ([main.go](main.go)): `gofi.New` loads `.env`, sets up logging
   and starts the components. The `iam` component builds the service from
   the environment (JWT, TTLs, session store) plus the ports the application
   gives it; `identity.Service()` returns it after `Build`. The server starts
   only in `ListenAndServe`, so the auth middleware and routes are added
   right after `Build`.
1. **Ports you implement** ([users.go](users.go)): `UserPort` finds users and
   checks passwords (Argon2id, `provider/password`, the same hasher as the
   dummy check iam runs for unknown emails); `TenantPort` says which tenants and roles a
   user has. Swap the maps for database queries and nothing else changes.
2. **What the component picks**: `provider/jwt` (HS256 tokens),
   `provider/memory` or, with `CACHE_TYPE=redis`, `provider/redis`
   (sessions); the example adds `provider/rbac/roles` (role → resource →
   actions). The Authenticate → SelectTenant ticket (always required,
   single-use) is signed with a key derived from `JWT_SECRET`.
3. **Login is two steps**: `Authenticate` checks the password and returns the
   tenants plus a signed ticket; `SelectTenant` opens the session and issues
   the tokens. With one tenant the handler runs both at once.
4. **Every request**: `ValidateToken` checks signature and expiry, then that
   the session is still active. Claims (`user`, `tenant`, `roles`,
   `session_id`) go into the context.
5. **Authorization**: `iam.RBAC().Enforce(claims, "reports", "read")`.
6. **Audit**: `Config.OnEvent` receives every login, failure, refresh,
   logout and suspicious activity; here they are logged.

For production: `JWT_SECRET` from a secret manager, `CACHE_TYPE=redis` for
the sessions, `COOKIE_SECURE=true` behind HTTPS, and a vault shared by the
instances (Redis too) in session mode.
