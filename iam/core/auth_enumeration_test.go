package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	iamconfig "github.com/joaoprofile/gofi-sdk-go/iam/config"
	"github.com/joaoprofile/gofi-sdk-go/iam/port"
	"github.com/joaoprofile/gofi-sdk-go/iam/provider/password"
	"github.com/joaoprofile/gofi-sdk-go/iam/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestAuthenticate_InactiveAccountHiddenWithoutPassword(t *testing.T) {
	user := &stubUserPort{
		user:        &types.User{ID: "u1", Active: false},
		validateErr: errors.New("wrong password"),
	}
	auth, _ := buildLocalAuth(user, &stubTenantPort{}, &stubTokenPort{})

	_, err := auth.Authenticate(context.Background(), port.AuthInput{Email: "a@b.com", Password: "guess"})
	assert.ErrorIs(t, err, ErrInvalidCredentials)
}

// dummyVerifierUser is a UserPort that verifies the dummy password itself.
type dummyVerifierUser struct {
	stubUserPort
	called string
}

func (u *dummyVerifierUser) DummyValidatePassword(_ context.Context, pw string) { u.called = pw }

func TestAuthenticate_UnknownEmailSpendsPasswordCheckTime(t *testing.T) {
	user := &dummyVerifierUser{stubUserPort: stubUserPort{findErr: errors.New("not found")}}
	auth := NewLocalAuth(LocalAuthConfig{User: user, Tenant: &stubTenantPort{}, Token: &stubTokenPort{}, Session: newMemSession()})
	_, err := auth.Authenticate(context.Background(), port.AuthInput{Email: "x@y.com", Password: "pw"})

	assert.ErrorIs(t, err, ErrInvalidCredentials)
	assert.Equal(t, "pw", user.called, "the UserPort's own dummy verifier runs")
}

// The default dummy used to be bcrypt cost 12 while the SDK recommends
// Argon2id: unknown emails answered at a different speed (user enumeration).
func TestDummyHash_MatchesRecommendedHasher(t *testing.T) {
	h := AuthConfig{}.dummyHash()
	require.True(t, strings.HasPrefix(h, "$argon2id$"), "dummy must be Argon2id: %s", h)
	p := password.DefaultParams
	assert.Contains(t, h, fmt.Sprintf("$m=%d,t=%d,p=%d$", p.Memory, p.Time, p.Threads))
	assert.False(t, p.NeedsRehash(h), "dummy must use exactly password.DefaultParams")
}

func TestDummyHash_Configurable(t *testing.T) {
	custom, err := bcrypt.GenerateFromPassword([]byte("x"), bcrypt.MinCost)
	require.NoError(t, err)
	cfg := AuthConfigFromSecurity(iamconfig.SecurityConfig{DummyPasswordHash: string(custom)})
	assert.Equal(t, string(custom), cfg.dummyHash(), "SecurityConfig.DummyPasswordHash wins")
}
