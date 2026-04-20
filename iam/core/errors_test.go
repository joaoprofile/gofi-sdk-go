package core

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrorSentinels_AreNotNil(t *testing.T) {
	assert.NotNil(t, ErrInvalidCredentials)
	assert.NotNil(t, ErrAccountInactive)
	assert.NotNil(t, ErrTokenExpired)
	assert.NotNil(t, ErrTokenInvalid)
	assert.NotNil(t, ErrSessionRevoked)
	assert.NotNil(t, ErrSessionNotFound)
	assert.NotNil(t, ErrSessionExpired)
	assert.NotNil(t, ErrAccessDenied)
	assert.NotNil(t, ErrTenantAccessDenied)
	assert.NotNil(t, ErrInvalidIDPState)
	assert.NotNil(t, ErrIDPCallbackFailed)
	assert.NotNil(t, ErrSessionPortRequired)
	assert.NotNil(t, ErrAccessTokenTTLExceeded)
	assert.NotNil(t, ErrRefreshTokenTTLExceeded)
	assert.NotNil(t, ErrJWTSecretTooShort)
	assert.NotNil(t, ErrProviderNotFound)
}

func TestErrorSentinels_HaveCorrectMessages(t *testing.T) {
	assert.Equal(t, "iam: invalid credentials", ErrInvalidCredentials.Error())
	assert.Equal(t, "iam: account inactive", ErrAccountInactive.Error())
	assert.Equal(t, "iam: token expired", ErrTokenExpired.Error())
	assert.Equal(t, "iam: token invalid", ErrTokenInvalid.Error())
	assert.Equal(t, "iam: session revoked", ErrSessionRevoked.Error())
	assert.Equal(t, "iam: session not found", ErrSessionNotFound.Error())
	assert.Equal(t, "iam: session expired", ErrSessionExpired.Error())
	assert.Equal(t, "iam: access denied", ErrAccessDenied.Error())
	assert.Equal(t, "iam: tenant access denied", ErrTenantAccessDenied.Error())
	assert.Equal(t, "iam: SessionPort is required — session cannot be nil", ErrSessionPortRequired.Error())
	assert.Equal(t, "iam: JWTSecret must be at least 32 bytes", ErrJWTSecretTooShort.Error())
	assert.Equal(t, "iam: IDP provider not registered", ErrProviderNotFound.Error())
}

func TestErrorSentinels_AreDistinct(t *testing.T) {
	errs := []error{
		ErrInvalidCredentials,
		ErrAccountInactive,
		ErrTokenExpired,
		ErrTokenInvalid,
		ErrSessionRevoked,
		ErrSessionNotFound,
		ErrSessionExpired,
		ErrAccessDenied,
		ErrTenantAccessDenied,
		ErrInvalidIDPState,
		ErrIDPCallbackFailed,
		ErrSessionPortRequired,
		ErrAccessTokenTTLExceeded,
		ErrRefreshTokenTTLExceeded,
		ErrJWTSecretTooShort,
		ErrProviderNotFound,
	}
	for i, a := range errs {
		for j, b := range errs {
			if i != j {
				assert.False(t, errors.Is(a, b), "errors at index %d and %d should be distinct", i, j)
			}
		}
	}
}
