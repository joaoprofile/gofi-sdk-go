// Package password hashes passwords with Argon2id (RFC 9106, OWASP's first
// choice) in the PHC string format and verifies both Argon2id and bcrypt
// hashes, so existing bcrypt users migrate on their next login:
//
//	if err := password.Verify(stored, pw); err != nil { return errInvalid }
//	if password.NeedsRehash(stored) {
//	    newHash, _ := password.Hash(pw) // persist newHash
//	}
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// ErrInvalid is returned for a wrong password or an unreadable hash; the two
// are deliberately indistinguishable.
var ErrInvalid = errors.New("iam/password: invalid credentials")

// Params are the Argon2id cost parameters.
type Params struct {
	Memory  uint32 // KiB
	Time    uint32 // iterations
	Threads uint8
	SaltLen uint32
	KeyLen  uint32
}

// DefaultParams follow the OWASP baseline (19 MiB, 2 iterations, 1 lane):
// strong while keeping concurrent logins affordable in small pods.
var DefaultParams = Params{Memory: 19 * 1024, Time: 2, Threads: 1, SaltLen: 16, KeyLen: 32}

// Upper bounds accepted when decoding a stored hash.
const (
	maxMemory = 1 << 20 // KiB (1 GiB)
	maxTime   = 64
	maxLen    = 1024
)

// Hash returns the Argon2id PHC string of password with DefaultParams.
func Hash(password string) (string, error) { return DefaultParams.Hash(password) }

// Hash returns the Argon2id PHC string of password with p.
func (p Params) Hash(password string) (string, error) {
	salt := make([]byte, p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("iam/password: salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, p.KeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Time, p.Threads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// Verify checks password against an Argon2id or bcrypt hash in constant time.
func Verify(hash, password string) error {
	if isBcrypt(hash) {
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
			return ErrInvalid
		}
		return nil
	}
	p, salt, key, err := decode(hash)
	if err != nil {
		return ErrInvalid
	}
	got := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, uint32(len(key))) // #nosec G115 -- decode caps len(key)
	if subtle.ConstantTimeCompare(got, key) != 1 {
		return ErrInvalid
	}
	return nil
}

// NeedsRehash reports whether hash should be replaced by a DefaultParams hash.
func NeedsRehash(hash string) bool { return DefaultParams.NeedsRehash(hash) }

// NeedsRehash is true for bcrypt, unreadable hashes and Argon2id hashes
// weaker than p.
func (p Params) NeedsRehash(hash string) bool {
	if isBcrypt(hash) {
		return true
	}
	h, salt, key, err := decode(hash)
	if err != nil {
		return true
	}
	return h.Memory < p.Memory || h.Time < p.Time || h.Threads < p.Threads ||
		uint32(len(salt)) < p.SaltLen || uint32(len(key)) < p.KeyLen // #nosec G115 -- decode caps both lengths
}

// Supported reports whether Verify can check hash (Argon2id within the
// accepted bounds, or bcrypt).
func Supported(hash string) bool {
	if isBcrypt(hash) {
		_, err := bcrypt.Cost([]byte(hash))
		return err == nil
	}
	_, _, _, err := decode(hash)
	return err == nil
}

func isBcrypt(hash string) bool {
	return strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$") || strings.HasPrefix(hash, "$2y$")
}

// decode parses $argon2id$v=19$m=..,t=..,p=..$salt$key.
func decode(hash string) (Params, []byte, []byte, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return Params{}, nil, nil, errors.New("not an argon2id hash")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Params{}, nil, nil, errors.New("unsupported argon2 version")
	}
	var p Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &p.Threads); err != nil {
		return Params{}, nil, nil, err
	}
	// Bounded so a tampered stored hash cannot make Verify allocate or spin without limit.
	if p.Memory == 0 || p.Time == 0 || p.Threads == 0 || p.Memory > maxMemory || p.Time > maxTime {
		return Params{}, nil, nil, errors.New("invalid argon2 parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) > maxLen {
		return Params{}, nil, nil, errors.New("invalid argon2 salt")
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 || len(key) > maxLen {
		return Params{}, nil, nil, errors.New("invalid argon2 key")
	}
	return p, salt, key, nil
}
