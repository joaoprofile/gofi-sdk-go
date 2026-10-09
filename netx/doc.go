// Package netx holds what the HTTP and gRPC transports share: the hardened
// server TLS (TLSConfig, ServerTLSConfig) and the request ID carried in the
// context (RequestIDKey, GetRequestID).
//
// The transports live in subpackages:
//
//   - netx/httpx: HTTP server (routes, middleware, CORS, rate limiting,
//     health) and HTTP client (retries, signing, response parsing).
//   - netx/grpcx: gRPC server and client, a separate module so services that
//     only speak HTTP never link gRPC.
//   - netx/awssign: AWS Signature V4 for the httpx client.
package netx
