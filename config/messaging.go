package config

import (
	"fmt"

	"github.com/joaoprofile/gofi/base/environment"
	"github.com/joaoprofile/gofi/msq/provider/kafka"
	"github.com/joaoprofile/gofi/msq/provider/oci"
	redisbroker "github.com/joaoprofile/gofi/msq/provider/redis"
)

// Kafka builds a kafka.Config from the MESSAGING_* environment variables.
func Kafka(env *environment.Environment) kafka.Config {
	return kafka.Config{
		Brokers:       []string{fmt.Sprintf("%s:%d", env.MessagingHost, env.MessagingPort)},
		User:          env.MessagingUser,
		Password:      env.MessagingPassword,
		UseTLS:        env.MessagingUseTLS,
		SASLMechanism: env.MessagingSASLMechanism,
	}
}

// OCIQueue builds an oci.Config from the MESSAGING_OCI_* environment variables.
func OCIQueue(env *environment.Environment) oci.Config {
	return oci.Config{
		TenancyID:   env.MessagingOCITenancyId,
		UserID:      env.MessagingOCIUserId,
		Region:      env.MessagingOCIRegion,
		FingerPrint: env.MessagingOCIFingerPrint,
		PrivateKey:  "OCI_PRIVATE_KEY", // TODO: source the private key from the env too
	}
}

// RedisBroker builds a redisbroker.Config from the CACHE_* environment
// variables (the Redis messaging broker reuses the cache connection settings).
func RedisBroker(env *environment.Environment) redisbroker.Config {
	return redisbroker.Config{
		Addr:     env.CacheURI,
		Password: env.CachePassword,
	}
}

// RabbitMQURL builds the AMQP connection URL from the MESSAGING_* environment
// variables. Pass it to rabbitmq.DialURL.
func RabbitMQURL(env *environment.Environment) string {
	return fmt.Sprintf("amqp://%s:%s@%s:%d/",
		env.MessagingUser,
		env.MessagingPassword,
		env.MessagingHost,
		env.MessagingPort,
	)
}
