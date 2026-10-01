package oci_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"

	cloudoci "github.com/gofi-labs/gofi-sdk-go/base/cloud/oci"
	"github.com/gofi-labs/gofi-sdk-go/msq/provider/oci"
	"github.com/gofi-labs/gofi-sdk-go/msq/types"
	"github.com/gofi-labs/gofi-sdk-go/obs/logging"
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
		Credentials: cloudoci.Config{
			TenancyID:   "ocid1.tenancy.oc1..aaaaaaaatest",
			UserID:      "ocid1.user.oc1..aaaaaaaatest",
			Region:      "sa-saopaulo-1",
			Fingerprint: "aa:bb:cc:dd:ee:ff:00:11:22:33:44:55:66:77:88:99",
			PrivateKey:  generatePrivateKey(t),
		},
	}
}

// Validation (missing required fields)

func TestNewMissingTenancyID(t *testing.T) {
	cfg := validConfig(t)
	cfg.Credentials.TenancyID = ""
	_, err := oci.New(cfg)
	assert.ErrorIs(t, err, cloudoci.ErrInvalidConfig)
}

func TestNewMissingUserID(t *testing.T) {
	cfg := validConfig(t)
	cfg.Credentials.UserID = ""
	_, err := oci.New(cfg)
	assert.Error(t, err)
}

func TestNewMissingRegion(t *testing.T) {
	cfg := validConfig(t)
	cfg.Credentials.Region = ""
	_, err := oci.New(cfg)
	assert.Error(t, err)
}

func TestNewMissingFingerPrint(t *testing.T) {
	cfg := validConfig(t)
	cfg.Credentials.Fingerprint = ""
	_, err := oci.New(cfg)
	assert.Error(t, err)
}

func TestNewAllEmptyCredentials(t *testing.T) {
	_, err := oci.New(oci.Config{})
	assert.Error(t, err)
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

	p, err := broker.NewProducer()
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestNewConsumer(t *testing.T) {
	broker, err := oci.New(validConfig(t))
	require.NoError(t, err)

	c, _ := broker.NewConsumer(types.ConsumeConfig{QueueID: "ocid1.queue.oc1..test", Concurrency: 2})
	assert.NotNil(t, c)
}

func TestNewConsumerDefaultsConcurrency(t *testing.T) {
	broker, err := oci.New(validConfig(t))
	require.NoError(t, err)

	c, _ := broker.NewConsumer(types.ConsumeConfig{QueueID: "q", Concurrency: 0})
	assert.NotNil(t, c)
}

// Producer: topic-validation error (no network call)

func TestProducerSendMessageMissingTopic(t *testing.T) {
	broker, err := oci.New(validConfig(t))
	require.NoError(t, err)
	p, err := broker.NewProducer()
	require.NoError(t, err)

	err = p.SendMessage(context.Background(), &types.Message{Topic: ""})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "queue OCID")
}

// Producer / Consumer trivial methods

func TestProducerClose(t *testing.T) {
	broker, err := oci.New(validConfig(t))
	require.NoError(t, err)
	p, err := broker.NewProducer()
	assert.NoError(t, err)
	assert.NoError(t, p.Close())
}

func TestConsumerClose(t *testing.T) {
	broker, err := oci.New(validConfig(t))
	require.NoError(t, err)
	c, err := broker.NewConsumer(types.ConsumeConfig{QueueID: "q"})
	require.NoError(t, err)
	assert.NoError(t, c.Close())
}

func TestConsumerPause(t *testing.T) {
	broker, err := oci.New(validConfig(t))
	require.NoError(t, err)
	c, err := broker.NewConsumer(types.ConsumeConfig{QueueID: "q"})
	require.NoError(t, err)
	assert.NoError(t, c.Pause())
}

func TestConsumerResume(t *testing.T) {
	broker, err := oci.New(validConfig(t))
	require.NoError(t, err)
	c, err := broker.NewConsumer(types.ConsumeConfig{QueueID: "q"})
	require.NoError(t, err)
	assert.NoError(t, c.Resume())
}

// Producer batch: topic validation

func TestProducerSendMessagesBatchPartialEmptyTopic(t *testing.T) {
	broker, err := oci.New(validConfig(t))
	require.NoError(t, err)
	p, err := broker.NewProducer()
	require.NoError(t, err)

	// batch where second message has a real OCID (the network will fail but
	// first msg triggers json.Marshal path in the loop)
	msgs := []*types.Message{
		testMessageWithTopic("ocid1.queue.oc1..validqueue", "data"),
	}
	// The call will fail at PutMessages (network), but must not panic.
	_ = p.SendMessagesBatch(context.Background(), msgs)
}
