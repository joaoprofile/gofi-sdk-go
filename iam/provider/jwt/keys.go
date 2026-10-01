package jwt

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"errors"
	"fmt"

	"github.com/gofi-labs/gofi-sdk-go/iam/core"
)

// minRSABits is the NIST minimum modulus for RSA signatures.
const minRSABits = 2048

// ErrInvalidKey is returned by NewProvider for a key of the wrong type or size.
var ErrInvalidKey = errors.New("iam/jwt: invalid key")

// validateKeys checks every key against the algorithm, so signing and
// verification never meet a key of the wrong type or strength at runtime.
func validateKeys(cfg Config) error {
	var check func(key any) error
	var err error
	switch cfg.Algorithm {
	case HS256:
		check, err = checkSecret, checkSecret(cfg.Secret)
	case RS256:
		check, err = checkRSAPublic, checkRSAPrivate(cfg.PrivateKey, cfg.PublicKey)
	case ES256:
		check, err = checkP256Public, checkP256Private(cfg.PrivateKey, cfg.PublicKey)
	default:
		return fmt.Errorf("iam/jwt: unsupported algorithm %s", cfg.Algorithm)
	}
	if err != nil {
		return err
	}
	for kid, key := range cfg.VerificationKeys {
		if err := check(key); err != nil {
			return fmt.Errorf("iam/jwt: verification key %q: %w", kid, err)
		}
	}
	return nil
}

func checkSecret(key any) error {
	secret, ok := key.([]byte)
	if !ok {
		return fmt.Errorf("%w: HS256 key must be []byte, got %T", ErrInvalidKey, key)
	}
	if len(secret) < 32 {
		return core.ErrJWTSecretTooShort
	}
	return nil
}

func checkRSAPrivate(priv, pub any) error {
	k, ok := priv.(*rsa.PrivateKey)
	if !ok {
		return fmt.Errorf("%w: RS256 PrivateKey must be *rsa.PrivateKey, got %T", ErrInvalidKey, priv)
	}
	if err := checkRSAPublic(pub); err != nil {
		return err
	}
	if !k.PublicKey.Equal(pub) {
		return fmt.Errorf("%w: RS256 PublicKey does not match PrivateKey", ErrInvalidKey)
	}
	return nil
}

func checkRSAPublic(key any) error {
	k, ok := key.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("%w: RS256 key must be *rsa.PublicKey, got %T", ErrInvalidKey, key)
	}
	if k.N == nil || k.N.BitLen() < minRSABits {
		return fmt.Errorf("%w: RSA key must be at least %d bits", ErrInvalidKey, minRSABits)
	}
	return nil
}

func checkP256Private(priv, pub any) error {
	k, ok := priv.(*ecdsa.PrivateKey)
	if !ok {
		return fmt.Errorf("%w: ES256 PrivateKey must be *ecdsa.PrivateKey, got %T", ErrInvalidKey, priv)
	}
	if err := checkP256Public(pub); err != nil {
		return err
	}
	if !k.PublicKey.Equal(pub) {
		return fmt.Errorf("%w: ES256 PublicKey does not match PrivateKey", ErrInvalidKey)
	}
	return nil
}

func checkP256Public(key any) error {
	k, ok := key.(*ecdsa.PublicKey)
	if !ok {
		return fmt.Errorf("%w: ES256 key must be *ecdsa.PublicKey, got %T", ErrInvalidKey, key)
	}
	if k.Curve != elliptic.P256() {
		return fmt.Errorf("%w: ES256 requires a P-256 key", ErrInvalidKey)
	}
	return nil
}
