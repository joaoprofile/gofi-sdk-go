# Security policy

## Supported versions

Only the latest minor release line of each module receives security fixes. Modules are
versioned together (`<module>/vX.Y.Z` tags), so upgrade every gofi module to the same version.

## Reporting a vulnerability

Do **not** open a public issue. Report privately through
[GitHub private vulnerability reporting](https://github.com/gofi-labs/gofi-sdk-go/security/advisories/new).

Include the affected module and version, a description of the impact and, when possible,
a minimal reproduction. We acknowledge reports within 3 business days and aim to ship a fix
for critical and high severity issues within 30 days, coordinating disclosure with the reporter.

## What CI checks on every change

- `govulncheck` on every module (known vulnerabilities in reachable code).
- `gosec` and `staticcheck` static analysis.
- `gitleaks` over the full git history.
- `go test -race` plus integration contracts against real brokers and PostgreSQL.
- Dependabot keeps Go modules and GitHub Actions current; actions are pinned by commit SHA.

## Hardening guidance for consumers

- Run with `APP_ENVIRONMENT=prod` (or `stage`). It disables `.env` loading and turns on the transport
  guard: `Build` fails when the database, cache, broker, OTLP exporter or object storage is reached
  without verified TLS, or when `TLS_INSECURE_SKIP_VERIFY` is set.
- `GOFI_ALLOW_INSECURE_TRANSPORT=<resource,...>` downgrades that refusal to a logged warning. Treat
  every use as a documented exception, not a default.
- If the pod itself is exposed without an ingress or mesh terminating TLS, set `HTTP_REQUIRE_TLS=true`
  so an HTTP server without TLS fails the build instead of logging a warning.
- Behind a load balancer, set `TrustedProxies` (`HTTP_TRUSTED_PROXIES`) explicitly; no proxy is
  trusted by default.
- Cross-origin protection (CSRF) is on for every route, trusting the route's CORS origins. Keep
  `AllowedOrigins` exact and never disable the protection on cookie-authenticated routes.
- Keep secrets out of `ClaimsExtra`: it travels inside the JWT. Use `SessionExtra` for server-side data.
- Store sessions in Redis (`CACHE_TYPE=redis`) so login throttling, single-use tenant tickets and
  revocation work across replicas; in-memory sessions are refused in `stage`/`prod`.
- Configure a `DeadLetterTopic` for every consumer; without one, poison messages are redelivered
  indefinitely instead of being dropped.
- Load secrets through `secret://` references or `*_FILE` variables instead of plain environment values.
- Never pass request data as SQL identifiers (sort fields, columns); map API names to columns with `filter.Mapping`.
- Keep presigned URL lifetimes short (minutes). OCI pre-authenticated requests cannot be revoked by
  the URL holder; `BUCKET_PRESIGN_MAX_TTL` lowers the 7-day cap enforced by `PresignGet`.
- Schedule jobs that run on several replicas with a `RedisLocker` and `ScheduleJobCtx`; pass
  `cronjob.FencingToken(ctx)` to the writes a job makes, so a run whose lease expired cannot
  overwrite the work of the next one.
