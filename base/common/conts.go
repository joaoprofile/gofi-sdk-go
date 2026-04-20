package common

const (
	AUTHORIZATION_HEADER = "Authorization"
	USER_ID_HEADER       = "X-User-Id"
	COMPANY_ID_HEADER    = "X-Company-Id"

	ACCEPT                  = "Accept"
	CONTENT_TYPE            = "content-type"
	APPLICATION_JSON        = "application/json"
	APPLICATION_URL_ENCODED = "application/x-www-form-urlencoded"
)

type LogLevel string

const (
	LogLevelError LogLevel = "error"
	LogLevelWarn  LogLevel = "warn"
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
)
