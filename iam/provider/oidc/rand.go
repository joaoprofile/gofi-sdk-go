package oidc

import (
	"crypto/rand"
	"io"
)

// cryptoRandRead is a wrapper for crypto/rand.Read that allows mocking in tests.
var cryptoRandRead = func(b []byte) (int, error) {
	return io.ReadFull(rand.Reader, b)
}
