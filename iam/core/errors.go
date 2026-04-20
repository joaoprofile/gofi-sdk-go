package core

import "errors"

var (
	// Authentication errors are intentionally generic to prevent user enumeration.
	ErrInvalidCredentials = errors.New("iam: invalid credentials")
	ErrAccountInactive    = errors.New("iam: account inactive")

	// Token errors.
	ErrTokenExpired  = errors.New("iam: token expired")
	ErrTokenInvalid  = errors.New("iam: token invalid")
	ErrSessionRevoked  = errors.New("iam: session revoked")
	ErrSessionNotFound = errors.New("iam: session not found")
	ErrSessionExpired  = errors.New("iam: session expired")

	// Authorization errors.
	ErrAccessDenied       = errors.New("iam: access denied")
	ErrTenantAccessDenied = errors.New("iam: tenant access denied")

	// IDP errors.
	ErrInvalidIDPState   = errors.New("iam: invalid idp state — possible CSRF")
	ErrIDPCallbackFailed = errors.New("iam: idp callback processing failed")

	// Configuration errors detected in New() before initialization.
	ErrSessionPortRequired     = errors.New("iam: SessionPort is required — session cannot be nil")
	ErrAccessTokenTTLExceeded  = errors.New("iam: AccessTokenTTL must be ≤ 60 minutes")
	ErrRefreshTokenTTLExceeded = errors.New("iam: RefreshTokenTTL must be ≤ 90 days")
	ErrJWTSecretTooShort       = errors.New("iam: JWTSecret must be at least 32 bytes")
	ErrProviderNotFound        = errors.New("iam: IDP provider not registered")
)
