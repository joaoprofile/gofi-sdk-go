package msq

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ProviderConfig is the provider-neutral connection that Open hands to a
// registered provider. The root config package fills it from the MESSAGING_*
// variables; each provider reads only its own fields. Printing or logging it
// (fmt, slog, JSON) redacts every secret.
type ProviderConfig struct {
	// Type selects the registered provider.
	Type BrokerType
	// Addr is the broker host:port (Kafka seed, AMQP or NATS server).
	Addr     string
	User     string
	Password string
	// UseTLS encrypts the connection (Kafka, RabbitMQ, NATS). Any field set in
	// TLS enables it too.
	UseTLS bool
	// TLS customizes the TLS connection: private CA, client certificate
	// (mTLS) and server name. Kafka, RabbitMQ, NATS and Redis apply it.
	TLS TLSConfig
	// ClientName identifies the connection on the server (NATS).
	ClientName string
	// SASLMechanism selects Kafka SASL: PLAIN (default), SCRAM-SHA-256 or SCRAM-SHA-512.
	SASLMechanism string
	// AllowPlaintextSASL lets Kafka send SASL PLAIN credentials without TLS.
	// Only for local development: the password travels in clear text.
	AllowPlaintextSASL bool
	// Exchange is the AMQP exchange (RabbitMQ).
	Exchange string
	// Encoding is the wire format (RabbitMQ); empty means envelope.
	Encoding Encoding

	Redis RedisConnection
	OCI   OCICredentials
}

// TLSEnabled reports whether the Kafka, RabbitMQ or NATS connection uses TLS.
func (c ProviderConfig) TLSEnabled() bool { return c.UseTLS || c.TLS.configured() }

// RedisTLSEnabled reports whether the Redis connection uses TLS.
func (c ProviderConfig) RedisTLSEnabled() bool { return c.Redis.UseTLS || c.TLS.configured() }

// Insecure reports whether the connection would send data in plaintext or
// skip certificate verification. SQS and OCI always use their SDK's HTTPS.
// gofi refuses an insecure broker in production.
func (c ProviderConfig) Insecure() bool {
	switch c.Type {
	case BrokerSQS, BrokerOCI:
		return false
	case BrokerRedis:
		return !c.RedisTLSEnabled() || c.TLS.InsecureSkipVerify
	default:
		return !c.TLSEnabled() || c.TLS.InsecureSkipVerify
	}
}

// RedisConnection configures the Redis provider.
type RedisConnection struct {
	Addr     string
	Password string
	UseTLS   bool
	// Mode is pubsub (default) or streams.
	Mode string
}

// OCICredentials configures the OCI Queue provider; see base/cloud/oci.
type OCICredentials struct {
	AuthMode    string
	Region      string
	TenancyID   string
	UserID      string
	Fingerprint string
	PrivateKey  string
}

// Opener builds a Broker from a ProviderConfig.
type Opener func(ctx context.Context, cfg ProviderConfig) (Broker, error)

var (
	openersMu sync.RWMutex
	openers   = map[BrokerType]Opener{}
)

// Register makes a provider available to Open. Provider packages call it from
// init, so importing one (e.g. _ ".../msq/provider/kafka") enables it and only
// imported providers are linked into the binary.
func Register(t BrokerType, o Opener) {
	openersMu.Lock()
	defer openersMu.Unlock()
	openers[t] = o
}

// Open builds the broker for cfg.Type. It fails when the type is not set or
// its provider package was not imported.
func Open(ctx context.Context, cfg ProviderConfig) (Broker, error) {
	if cfg.Type == "" {
		return nil, errors.New("msq: provider type is not set")
	}
	openersMu.RLock()
	o, ok := openers[cfg.Type]
	openersMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("msq: provider %q is not registered; import _ \"github.com/gofi-labs/gofi-sdk-go/msq/provider/%s\"", cfg.Type, cfg.Type)
	}
	return o(ctx, cfg)
}
