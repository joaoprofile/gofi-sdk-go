package types

import "time"

// EventType identifies the type of security or authentication event emitted by the SDK.
type EventType string

const (
	EventLogin              EventType = "auth.login"
	EventLoginFailed        EventType = "auth.login_failed"
	EventIDPLogin           EventType = "auth.idp_login"
	EventIDPLoginFailed     EventType = "auth.idp_login_failed"
	EventNewUser            EventType = "auth.new_user" // first login via IDP
	EventTenantSelected     EventType = "auth.tenant_selected"
	EventLogout             EventType = "auth.logout"
	EventLogoutAll          EventType = "auth.logout_all"
	EventTokenRefreshed     EventType = "auth.token_refreshed"
	EventTokenRefreshFailed EventType = "auth.token_refresh_failed"
	EventAccessDenied       EventType = "authz.access_denied"
	EventTenantAccessDenied EventType = "authz.tenant_access_denied"
	EventTokenValidated     EventType = "token.validated"
	EventTokenInvalid       EventType = "token.invalid"
	EventSessionRevoked     EventType = "session.revoked"
	EventSuspiciousActivity EventType = "security.suspicious" // refresh token reuse or hash mismatch
)

// IAMEvent is emitted by the SDK for each relevant security action.
// The developer decides where to persist it via Config.OnEvent.
// Never includes passwords, raw tokens, or refresh tokens.
type IAMEvent struct {
	Type      EventType
	UserID    string
	TenantID  string
	Module    string
	SessionID string
	Provider  string // "local", "google", "github", etc.

	IPAddress string
	UserAgent string
	DeviceID  string

	Timestamp time.Time
	Error     error          // nil on success events
	Extra     map[string]any // additional event-specific metadata
}
