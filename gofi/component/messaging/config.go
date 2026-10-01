package messaging

import (
	"cmp"
	"net"
	"strconv"

	"github.com/gofi-labs/gofi-sdk-go/base/environment"
	"github.com/gofi-labs/gofi-sdk-go/msq"
)

// ServiceDefaultsFromEnv fills the msq.Config consumer defaults left at zero
// from MESSAGING_MAX_DELIVERIES and MESSAGING_HANDLER_TIMEOUT.
func ServiceDefaultsFromEnv(cfg msq.Config, env *environment.Environment) msq.Config {
	cfg.MaxDeliveries = cmp.Or(cfg.MaxDeliveries, env.MessagingMaxDeliveries)
	cfg.HandlerTimeout = cmp.Or(cfg.HandlerTimeout, env.MessagingHandlerTimeout)
	return cfg
}

// ConfigFromEnv builds an msq.ProviderConfig from the MESSAGING_* variables; the
// Redis provider reuses the CACHE_* connection. Open it with msq.Open after
// importing the provider package (msq/provider/<type>).
func ConfigFromEnv(env *environment.Environment) msq.ProviderConfig {
	return msq.ProviderConfig{
		Type:     msq.BrokerType(env.GetMessagingProvider()),
		Addr:     net.JoinHostPort(env.MessagingHost, strconv.Itoa(env.MessagingPort)),
		User:     env.MessagingUser,
		Password: env.MessagingPassword,
		UseTLS:   env.MessagingUseTLS,
		TLS: msq.TLSConfig{
			CAFile:             env.MessagingTLSCAFile,
			CertFile:           env.MessagingTLSCertFile,
			KeyFile:            env.MessagingTLSKeyFile,
			ServerName:         env.MessagingTLSServerName,
			InsecureSkipVerify: env.MessagingTLSInsecureSkipVerify,
		},
		ClientName:         env.AppName,
		SASLMechanism:      env.MessagingSASLMechanism,
		AllowPlaintextSASL: env.MessagingAllowPlaintextSASL,
		Encoding:           msq.Encoding(env.MessagingEncoding),
		Redis: msq.RedisConnection{
			Addr:     env.CacheURI,
			Password: env.CachePassword,
			UseTLS:   env.CacheUseTLS,
			Mode:     env.MessagingRedisMode,
		},
		OCI: msq.OCICredentials{
			AuthMode:    env.MessagingOCIAuthMode,
			Region:      env.MessagingOCIRegion,
			TenancyID:   env.MessagingOCITenancyId,
			UserID:      env.MessagingOCIUserId,
			Fingerprint: env.MessagingOCIFingerPrint,
			// OCI_PRIVATE_KEY is the fallback API key.
			PrivateKey: cmp.Or(env.MessagingOCIPrivateKey, env.OCIPrivateKey),
		},
	}
}
