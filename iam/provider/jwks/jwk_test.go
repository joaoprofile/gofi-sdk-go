package jwks

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"math/big"
	"testing"
)

func TestJWKKey_RSAPublicKey(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	e := priv.E
	eBytes := []byte{byte(e >> 16), byte(e >> 8), byte(e)}
	for len(eBytes) > 1 && eBytes[0] == 0 {
		eBytes = eBytes[1:]
	}

	k := &jwkKey{
		Kty: "RSA",
		N:   base64.RawURLEncoding.EncodeToString(priv.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(eBytes),
	}

	pub, err := k.PublicKey()
	if err != nil {
		t.Fatalf("PublicKey() error: %v", err)
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		t.Fatal("expected *rsa.PublicKey")
	}
	if rsaPub.N.Cmp(priv.N) != 0 {
		t.Error("N mismatch")
	}
}

func TestJWKKey_RSAPublicKey_InvalidN(t *testing.T) {
	k := &jwkKey{Kty: "RSA", N: "!invalid!", E: "AQAB"}
	_, err := k.PublicKey()
	if err == nil {
		t.Fatal("expected error for invalid N")
	}
}

func TestJWKKey_RSAPublicKey_InvalidE(t *testing.T) {
	k := &jwkKey{Kty: "RSA", N: base64.RawURLEncoding.EncodeToString(big.NewInt(42).Bytes()), E: "!invalid!"}
	_, err := k.PublicKey()
	if err == nil {
		t.Fatal("expected error for invalid E")
	}
}

func TestJWKKey_ECPublicKey_P256(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	k := &jwkKey{
		Kty: "EC",
		Crv: "P-256",
		X:   base64.RawURLEncoding.EncodeToString(priv.X.Bytes()),
		Y:   base64.RawURLEncoding.EncodeToString(priv.Y.Bytes()),
	}

	pub, err := k.PublicKey()
	if err != nil {
		t.Fatalf("PublicKey() error: %v", err)
	}
	if _, ok := pub.(*ecdsa.PublicKey); !ok {
		t.Fatal("expected *ecdsa.PublicKey")
	}
}

func TestJWKKey_ECPublicKey_P384(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	k := &jwkKey{
		Kty: "EC",
		Crv: "P-384",
		X:   base64.RawURLEncoding.EncodeToString(priv.X.Bytes()),
		Y:   base64.RawURLEncoding.EncodeToString(priv.Y.Bytes()),
	}

	_, err = k.PublicKey()
	if err != nil {
		t.Fatalf("PublicKey() error: %v", err)
	}
}

func TestJWKKey_ECPublicKey_P521(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	k := &jwkKey{
		Kty: "EC",
		Crv: "P-521",
		X:   base64.RawURLEncoding.EncodeToString(priv.X.Bytes()),
		Y:   base64.RawURLEncoding.EncodeToString(priv.Y.Bytes()),
	}

	_, err = k.PublicKey()
	if err != nil {
		t.Fatalf("PublicKey() error: %v", err)
	}
}

func TestJWKKey_ECPublicKey_InvalidX(t *testing.T) {
	k := &jwkKey{Kty: "EC", Crv: "P-256", X: "!invalid!", Y: "AAAA"}
	_, err := k.PublicKey()
	if err == nil {
		t.Fatal("expected error for invalid X")
	}
}

func TestJWKKey_ECPublicKey_InvalidY(t *testing.T) {
	k := &jwkKey{
		Kty: "EC",
		Crv: "P-256",
		X:   base64.RawURLEncoding.EncodeToString(big.NewInt(1).Bytes()),
		Y:   "!invalid!",
	}
	_, err := k.PublicKey()
	if err == nil {
		t.Fatal("expected error for invalid Y")
	}
}

func TestJWKKey_ECPublicKey_UnsupportedCurve(t *testing.T) {
	k := &jwkKey{
		Kty: "EC",
		Crv: "P-999",
		X:   base64.RawURLEncoding.EncodeToString(big.NewInt(1).Bytes()),
		Y:   base64.RawURLEncoding.EncodeToString(big.NewInt(1).Bytes()),
	}
	_, err := k.PublicKey()
	if err == nil {
		t.Fatal("expected error for unsupported curve")
	}
}

func TestJWKKey_UnsupportedKeyType(t *testing.T) {
	k := &jwkKey{Kty: "oct"}
	_, err := k.PublicKey()
	if err == nil {
		t.Fatal("expected error for unsupported key type")
	}
}
