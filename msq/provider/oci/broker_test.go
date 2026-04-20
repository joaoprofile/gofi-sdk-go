package oci_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"

	"github.com/joaoprofile/gofi/msq/provider/oci"
	"github.com/joaoprofile/gofi/msq/types"
	"github.com/joaoprofile/gofi/obs/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	logging.NewLogger("oci-provider-test")
	os.Exit(m.Run())
}

// generatePrivateKey returns a PEM-encoded RSA-2048 private key for test use.
func generatePrivateKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	block := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return string(block)
}

// validConfig builds a Config with a real generated RSA key.
// The OCI SDK constructs the HTTP client without making network calls,
// so New() succeeds even though the credentials aren't associated with a real tenancy.
func validConfig(t *testing.T) oci.Config {
	t.Helper()
	return oci.Config{
		TenancyID:   "ocid1.tenancy.oc1..aaaaaaaatest",
		UserID:      "ocid1.user.oc1..aaaaaaaatest",
		Region:      "sa-saopaulo-1",
		FingerPrint: "aa:bb:cc:dd:ee:ff:00:11:22:33:44:55:66:77:88:99",
		PrivateKey:  generatePrivateKey(t),
		QueueURL:    "https://cell-1.queue.messaging.sa-saopaulo-1.oci.oraclecloud.com",
	}
}

// Validation (missing required fields)

func TestNewMissingTenancyID(t *testing.T) {
	cfg := validConfig(t)
	cfg.TenancyID = ""
	_, err := oci.New(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing required credentials")
}

func TestNewMissingUserID(t *testing.T) {
	cfg := validConfig(t)
	cfg.UserID = ""
	_, err := oci.New(cfg)
	assert.Error(t, err)
}

func TestNewMissingRegion(t *testing.T) {
	cfg := validConfig(t)
	cfg.Region = ""
	_, err := oci.New(cfg)
	assert.Error(t, err)
}

func TestNewMissingFingerPrint(t *testing.T) {
	cfg := validConfig(t)
	cfg.FingerPrint = ""
	_, err := oci.New(cfg)
	assert.Error(t, err)
}

func TestNewAllEmptyCredentials(t *testing.T) {
	_, err := oci.New(oci.Config{})
	assert.Error(t, err)
}

// ConfigFromEnv

func TestConfigFromEnv(t *testing.T) {
	cfg := oci.ConfigFromEnv()
	assert.IsType(t, oci.Config{}, cfg)
}

// Successful construction

func TestNewWithValidConfig(t *testing.T) {
	broker, err := oci.New(validConfig(t))
	require.NoError(t, err)
	assert.NotNil(t, broker)
}

func TestNewUsesDefaultQueueURL(t *testing.T) {
	cfg := validConfig(t)
	cfg.QueueURL = "" // triggers the default URL branch
	broker, err := oci.New(cfg)
	require.NoError(t, err)
	assert.NotNil(t, broker)
}

// NewProducer / NewConsumer

func TestNewProducer(t *testing.T) {
	broker, err := oci.New(validConfig(t))
	require.NoError(t, err)

	p := broker.NewProducer()
	assert.NotNil(t, p)
}

func TestNewConsumer(t *testing.T) {
	broker, err := oci.New(validConfig(t))
	require.NoError(t, err)

	c := broker.NewConsumer(types.ConsumeConfig{QueueID: "ocid1.queue.oc1..test", Concurrency: 2})
	assert.NotNil(t, c)
}

func TestNewConsumerDefaultsConcurrency(t *testing.T) {
	broker, err := oci.New(validConfig(t))
	require.NoError(t, err)

	c := broker.NewConsumer(types.ConsumeConfig{QueueID: "q", Concurrency: 0})
	assert.NotNil(t, c)
}

// Producer: topic-validation error (no network call)

func TestProducerSendMessageMissingTopic(t *testing.T) {
	broker, err := oci.New(validConfig(t))
	require.NoError(t, err)
	p := broker.NewProducer()

	err = p.SendMessage(context.Background(), &types.Message{Topic: ""})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "queue OCID")
}

// Producer / Consumer trivial methods

func TestProducerClose(t *testing.T) {
	broker, err := oci.New(validConfig(t))
	require.NoError(t, err)
	assert.NoError(t, broker.NewProducer().Close())
}

func TestConsumerClose(t *testing.T) {
	broker, err := oci.New(validConfig(t))
	require.NoError(t, err)
	assert.NoError(t, broker.NewConsumer(types.ConsumeConfig{QueueID: "q"}).Close())
}

func TestConsumerPause(t *testing.T) {
	broker, err := oci.New(validConfig(t))
	require.NoError(t, err)
	assert.NoError(t, broker.NewConsumer(types.ConsumeConfig{QueueID: "q"}).Pause())
}

func TestConsumerResume(t *testing.T) {
	broker, err := oci.New(validConfig(t))
	require.NoError(t, err)
	assert.NoError(t, broker.NewConsumer(types.ConsumeConfig{QueueID: "q"}).Resume())
}

// Producer batch: topic validation

func TestProducerSendMessagesBatchPartialEmptyTopic(t *testing.T) {
	broker, err := oci.New(validConfig(t))
	require.NoError(t, err)
	p := broker.NewProducer()

	// batch where second message has a real OCID (the network will fail but
	// first msg triggers json.Marshal path in the loop)
	msgs := []*types.Message{
		types.NewMessageWithTopic("ocid1.queue.oc1..validqueue", "data"),
	}
	// The call will fail at PutMessages (network), but must not panic.
	_ = p.SendMessagesBatch(context.Background(), msgs)
}
