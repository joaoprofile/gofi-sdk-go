package port

import (
	"context"

	"github.com/gofi-labs/gofi-sdk-go/iam/types"
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
	// Look up by Provider+ExternalID; linking to an existing account by Email
	// must be refused unless identity.EmailVerified is true, otherwise anyone
	// who controls an IDP account with the victim's email takes over the account.
	FindOrCreateByExternalIdentity(ctx context.Context, identity types.ExternalIdentity) (*types.User, error)
}
