// Package httpx serves and calls HTTP APIs: a chi-based server with hardened
// TLS (netx.TLSConfig), request IDs, error-only logging, CORS, cross-origin
// protection, rate limiting, health endpoints, OpenTelemetry and graceful
// shutdown, and a client with retries, request signing and safe response
// parsing.
//
// gofi services declare the server with the gofi/component/httpserver
// component.
package httpx
