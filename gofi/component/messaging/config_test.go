package messaging

import (
	"testing"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	"github.com/joaoprofile/gofi-sdk-go/msq"
	"github.com/stretchr/testify/assert"
)

func TestMessaging(t *testing.T) {
	c := ConfigFromEnv(&environment.Environment{
		AppName: "billing", MessagingProvider: "kafka", MessagingHost: "mq", MessagingPort: 9092,
		MessagingUser: "u", MessagingPassword: "p", MessagingUseTLS: true, MessagingSASLMechanism: "SCRAM-SHA-256",
		MessagingEncoding: "cloudevents", MessagingRedisMode: "streams",
		CacheURI: "redis:6379", CachePassword: "rp", CacheUseTLS: true,
		MessagingOCIAuthMode: "workload_identity", MessagingOCIRegion: "sa-saopaulo-1",
		MessagingOCITenancyId: "ten", MessagingOCIUserId: "usr", MessagingOCIFingerPrint: "aa:bb",
	})
	assert.Equal(t, msq.BrokerKafka, c.Type)
	assert.Equal(t, "mq:9092", c.Addr)
	assert.Equal(t, "billing", c.ClientName)
	assert.Equal(t, msq.EncodingCloudEvents, c.Encoding)
	assert.Equal(t, msq.RedisConnection{Addr: "redis:6379", Password: "rp", UseTLS: true, Mode: "streams"}, c.Redis)
	assert.Equal(t, msq.OCICredentials{AuthMode: "workload_identity", Region: "sa-saopaulo-1", TenancyID: "ten", UserID: "usr", Fingerprint: "aa:bb"}, c.OCI)
	assert.True(t, c.UseTLS)
	assert.Equal(t, "SCRAM-SHA-256", c.SASLMechanism)
}

func TestMessaging_TLS(t *testing.T) {
	c := ConfigFromEnv(&environment.Environment{
		MessagingTLSCAFile: "/ca.pem", MessagingTLSCertFile: "/c.pem", MessagingTLSKeyFile: "/k.pem",
		MessagingTLSServerName: "mq.internal", MessagingTLSInsecureSkipVerify: true, MessagingAllowPlaintextSASL: true,
	})
	assert.Equal(t, msq.TLSConfig{CAFile: "/ca.pem", CertFile: "/c.pem", KeyFile: "/k.pem", ServerName: "mq.internal", InsecureSkipVerify: true}, c.TLS)
	assert.True(t, c.AllowPlaintextSASL)
}

func TestServiceDefaultsFromEnv(t *testing.T) {
	env := &environment.Environment{MessagingMaxDeliveries: 5, MessagingHandlerTimeout: time.Minute}
	got := ServiceDefaultsFromEnv(msq.Config{}, env)
	assert.Equal(t, 5, got.MaxDeliveries)
	assert.Equal(t, time.Minute, got.HandlerTimeout)

	got = ServiceDefaultsFromEnv(msq.Config{MaxDeliveries: 3, HandlerTimeout: time.Second}, env)
	assert.Equal(t, 3, got.MaxDeliveries, "code wins over env")
	assert.Equal(t, time.Second, got.HandlerTimeout)
}

func TestMessaging_OCIPrivateKeyFallback(t *testing.T) {
	assert.Equal(t, "pem-a", ConfigFromEnv(&environment.Environment{MessagingOCIPrivateKey: "pem-a", OCIPrivateKey: "pem-b"}).OCI.PrivateKey)
	assert.Equal(t, "pem-b", ConfigFromEnv(&environment.Environment{OCIPrivateKey: "pem-b"}).OCI.PrivateKey)
}
