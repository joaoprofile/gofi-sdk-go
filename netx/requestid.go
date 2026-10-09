package netx

import (
	"context"
	"crypto/rand"
)

type contextKey string

// RequestIDKey is the context key under which the HTTP and gRPC servers store
// the request ID.
const RequestIDKey contextKey = "request_id"

// maxRequestIDLen bounds a client-supplied request ID.
const maxRequestIDLen = 64

// GetRequestID returns the request ID the HTTP or gRPC server stored in ctx.
func GetRequestID(ctx context.Context) string {
	id, _ := ctx.Value(RequestIDKey).(string)
	return id
}

// ValidRequestID accepts 1-64 characters of [A-Za-z0-9._-], so a client ID
// can neither forge log fields nor bloat them.
func ValidRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

// NewRequestID returns 128 random bits (crypto/rand, base32).
func NewRequestID() string {
	return rand.Text()
}
