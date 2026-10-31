package jwks

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
)

// MinRSABits rejects JWKS keys below the NIST minimum for signatures.
const MinRSABits = 2048

// jwkKey represents a key in JWK (JSON Web Key) format.
type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// PublicKey returns *rsa.PublicKey or *ecdsa.PublicKey; symmetric keys are unsupported.
func (k *jwkKey) PublicKey() (any, error) {
	switch k.Kty {
	case "RSA":
		return k.rsaPublicKey()
	case "EC":
		return k.ecPublicKey()
	default:
		return nil, fmt.Errorf("unsupported key type: %s", k.Kty)
	}
}

func (k *jwkKey) rsaPublicKey() (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, err
	}

	n := new(big.Int).SetBytes(nBytes)
	if n.BitLen() < MinRSABits {
		return nil, fmt.Errorf("RSA key too small: %d bits", n.BitLen())
	}
	// Bounded before accumulating so a long exponent cannot overflow int.
	if len(eBytes) == 0 || len(eBytes) > 4 {
		return nil, errors.New("invalid RSA exponent")
	}
	var eInt int
	for _, b := range eBytes {
		eInt = eInt*256 + int(b)
	}
	if eInt < 3 || eInt%2 == 0 {
		return nil, errors.New("invalid RSA exponent")
	}

	return &rsa.PublicKey{N: n, E: eInt}, nil
}

func (k *jwkKey) ecPublicKey() (*ecdsa.PublicKey, error) {
	xBytes, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, err
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(k.Y)
	if err != nil {
		return nil, err
	}

	var curve elliptic.Curve
	switch k.Crv {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return nil, fmt.Errorf("unsupported EC curve: %s", k.Crv)
	}

	size := (curve.Params().BitSize + 7) / 8
	if len(xBytes) > size || len(yBytes) > size {
		return nil, errors.New("invalid EC coordinates")
	}
	// Uncompressed SEC 1 point; parsing rejects coordinates that are not on the curve.
	point := make([]byte, 1+2*size)
	point[0] = 4
	copy(point[1+size-len(xBytes):1+size], xBytes)
	copy(point[1+2*size-len(yBytes):], yBytes)
	return ecdsa.ParseUncompressedPublicKey(curve, point)
}
