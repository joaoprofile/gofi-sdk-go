package oidc

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"testing"
)

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func TestJWK_RSARejectsWeakKeys(t *testing.T) {
	small, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	strong, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	n := b64(strong.N.Bytes())
	cases := map[string]jwkKey{
		"1024-bit modulus":   {Kty: "RSA", N: b64(small.N.Bytes()), E: "AQAB"},
		"exponent 1":         {Kty: "RSA", N: n, E: b64([]byte{1})},
		"even exponent":      {Kty: "RSA", N: n, E: b64([]byte{1, 0, 0})},
		"overflowing length": {Kty: "RSA", N: n, E: b64([]byte{1, 0, 0, 0, 0, 0, 0, 0, 1})},
		"empty exponent":     {Kty: "RSA", N: n, E: ""},
	}
	for name, k := range cases {
		if _, err := k.rsaPublicKey(); err == nil {
			t.Errorf("%s: must be rejected", name)
		}
	}
	if _, err := (&jwkKey{Kty: "RSA", N: n, E: "AQAB"}).rsaPublicKey(); err != nil {
		t.Fatalf("2048-bit key with e=65537 must be accepted: %v", err)
	}
}

func TestJWK_ECRejectsPointsOffTheCurve(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := priv.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	x, y := raw[1:33], raw[33:]

	good := jwkKey{Kty: "EC", Crv: "P-256", X: b64(x), Y: b64(y)}
	pub, err := good.ecPublicKey()
	if err != nil {
		t.Fatalf("valid P-256 key must be accepted: %v", err)
	}
	if !pub.Equal(&priv.PublicKey) {
		t.Fatal("parsed key differs from the original")
	}

	badY := append([]byte(nil), y...)
	badY[len(badY)-1] ^= 1
	cases := map[string]jwkKey{
		"off-curve point":   {Kty: "EC", Crv: "P-256", X: b64(x), Y: b64(badY)},
		"oversized x":       {Kty: "EC", Crv: "P-256", X: b64(append([]byte{1}, x...)), Y: b64(y)},
		"unsupported curve": {Kty: "EC", Crv: "P-224", X: b64(x), Y: b64(y)},
	}
	for name, k := range cases {
		if _, err := k.ecPublicKey(); err == nil {
			t.Errorf("%s: must be rejected", name)
		}
	}
}
