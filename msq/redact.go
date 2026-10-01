package msq

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

// redactedValue replaces a secret in printed or logged configuration.
const redactedValue = "[REDACTED]"

func redact(s string) string {
	if s == "" {
		return ""
	}
	return redactedValue
}

// The aliases drop the methods below, so formatting them does not recurse.
type (
	plainProviderConfig  ProviderConfig
	plainRedisConnection RedisConnection
	plainOCICredentials  OCICredentials
)

func (c ProviderConfig) redacted() plainProviderConfig {
	c.Password = redact(c.Password)
	c.Redis.Password = redact(c.Redis.Password)
	c.OCI.PrivateKey = redact(c.OCI.PrivateKey)
	return plainProviderConfig(c)
}

// String implements fmt.Stringer with secrets redacted.
func (c ProviderConfig) String() string { return fmt.Sprintf("%+v", c.redacted()) }

// GoString implements fmt.GoStringer with secrets redacted.
func (c ProviderConfig) GoString() string { return fmt.Sprintf("%#v", c.redacted()) }

// LogValue implements slog.LogValuer with secrets redacted.
func (c ProviderConfig) LogValue() slog.Value { return slog.StringValue(c.String()) }

// MarshalJSON encodes the configuration with secrets redacted.
func (c ProviderConfig) MarshalJSON() ([]byte, error) { return json.Marshal(c.redacted()) }

func (c RedisConnection) redacted() plainRedisConnection {
	c.Password = redact(c.Password)
	return plainRedisConnection(c)
}

// String implements fmt.Stringer with the password redacted.
func (c RedisConnection) String() string { return fmt.Sprintf("%+v", c.redacted()) }

// GoString implements fmt.GoStringer with the password redacted.
func (c RedisConnection) GoString() string { return fmt.Sprintf("%#v", c.redacted()) }

// LogValue implements slog.LogValuer with the password redacted.
func (c RedisConnection) LogValue() slog.Value { return slog.StringValue(c.String()) }

// MarshalJSON encodes the connection with the password redacted.
func (c RedisConnection) MarshalJSON() ([]byte, error) { return json.Marshal(c.redacted()) }

func (c OCICredentials) redacted() plainOCICredentials {
	c.PrivateKey = redact(c.PrivateKey)
	return plainOCICredentials(c)
}

// String implements fmt.Stringer with the private key redacted.
func (c OCICredentials) String() string { return fmt.Sprintf("%+v", c.redacted()) }

// GoString implements fmt.GoStringer with the private key redacted.
func (c OCICredentials) GoString() string { return fmt.Sprintf("%#v", c.redacted()) }

// LogValue implements slog.LogValuer with the private key redacted.
func (c OCICredentials) LogValue() slog.Value { return slog.StringValue(c.String()) }

// MarshalJSON encodes the credentials with the private key redacted.
func (c OCICredentials) MarshalJSON() ([]byte, error) { return json.Marshal(c.redacted()) }
