package core

import "errors"

var (
	// Authentication errors are intentionally generic to prevent user enumeration.
	ErrInvalidCredentials = errors.New("iam: invalid credentials")
	ErrAccountInactive    = errors.New("iam: account inactive")
	// ErrTooManyAttempts is returned while a LoginThrottler lockout is active;
	// it is the same for known and unknown emails.
	ErrTooManyAttempts = errors.New("iam: too many login attempts, try again later")

	// Token errors.
	ErrTokenExpired    = errors.New("iam: token expired")
	ErrTokenInvalid    = errors.New("iam: token invalid")
	ErrSessionRevoked  = errors.New("iam: session revoked")
	ErrSessionNotFound = errors.New("iam: session not found")
	ErrSessionExpired  = errors.New("iam: session expired")

	// Authorization errors.
	ErrAccessDenied        = errors.New("iam: access denied")
	ErrTenantAccessDenied  = errors.New("iam: tenant access denied")
	ErrInvalidTenantTicket = errors.New("iam: invalid or expired tenant ticket")

	// IDP errors.
	ErrInvalidIDPState   = errors.New("iam: invalid idp state — possible CSRF")
	ErrInvalidIDPNonce   = errors.New("iam: invalid idp nonce — possible token replay")
	ErrIDPCallbackFailed = errors.New("iam: idp callback processing failed")

	// Configuration errors detected in New() before initialization.
	ErrSessionPortRequired        = errors.New("iam: SessionPort is required — session cannot be nil")
	ErrAccessTokenTTLExceeded     = errors.New("iam: AccessTokenTTL must be ≤ 60 minutes")
	ErrRefreshTokenTTLExceeded    = errors.New("iam: RefreshTokenTTL must be ≤ 90 days")
	ErrJWTSecretTooShort          = errors.New("iam: JWTSecret must be at least 32 bytes")
	ErrTenantTicketSecretTooShort = errors.New("iam: TenantTicketSecret must be at least 32 bytes")
	ErrTenantTicketSecretRequired = errors.New("iam: TenantTicketSecret is required when login is enabled")
	ErrSessionMaxLifetimeExceeded = errors.New("iam: SessionMaxLifetime must be ≤ 90 days")
	ErrClockSkewExceeded          = errors.New("iam: ClockSkew/Leeway must be ≤ 5 minutes")
	ErrProviderNotFound           = errors.New("iam: IDP provider not registered")

	// ErrLoginPortsRequired is returned by the login operations when the
	// service was built without a UserPort or TenantPort.
	ErrLoginPortsRequired = errors.New("iam: UserPort and TenantPort are required for login; set User and Tenant in the config")
)
