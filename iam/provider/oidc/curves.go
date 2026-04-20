package oidc

import (
	"crypto/elliptic"
	"crypto/rand"
	"io"
)

// cryptoRandRead is a wrapper for crypto/rand.Read that allows mocking in tests.
var cryptoRandRead = func(b []byte) (int, error) {
	return io.ReadFull(rand.Reader, b)
}

func ellipticP256() elliptic.Curve { return elliptic.P256() }
func ellipticP384() elliptic.Curve { return elliptic.P384() }
func ellipticP521() elliptic.Curve { return elliptic.P521() }
