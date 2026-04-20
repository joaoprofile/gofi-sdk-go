package port

import (
	"context"

	"github.com/joaoprofile/gofi/iam/types"
)

// UserPort abstracts the user repository.
// The SDK has no opinion about where users are stored.
// The implementation is the responsibility of the developer.
type UserPort interface {
	FindByID(ctx context.Context, userID string) (*types.User, error)
	FindByEmail(ctx context.Context, email string) (*types.User, error)

	// ValidatePassword verifies the password against the stored hash.
	// The implementation must use timing-safe comparison such as bcrypt.CompareHashAndPassword.
	ValidatePassword(ctx context.Context, userID, password string) error

	// FindOrCreateByExternalIdentity is called during the social IDP callback.
	// If the user does not exist, it must create one. Must be idempotent.
	FindOrCreateByExternalIdentity(ctx context.Context, identity types.ExternalIdentity) (*types.User, error)
}
