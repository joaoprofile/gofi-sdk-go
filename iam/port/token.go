package port

import "github.com/joaoprofile/gofi/iam/types"

// TokenPort abstracts token generation and parsing.
// JWT is only one implementation of this interface and may be replaced
// by Paseto, opaque HMAC tokens, or any other mechanism.
type TokenPort interface {
	// IssueAccessToken issues a signed access token with the provided claims.
	IssueAccessToken(claims types.Claims) (string, error)

	// IssueRefreshToken generates a high-entropy opaque token.
	// The format is {sessionID}.{base64url(randomBytes)} to allow efficient
	// session lookup without a secondary index.
	// The core stores only the SHA-256 hash of the token in the SessionPort.
	IssueRefreshToken(claims types.Claims) (string, error)

	// ParseToken validates signature and expiry. Does not validate the session —
	// that is the responsibility of AuthPort.ValidateToken.
	ParseToken(token string) (*types.Claims, error)
}
