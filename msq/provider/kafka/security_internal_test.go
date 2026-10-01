package kafka

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/IBM/sarama"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_RefusesUnsafeSASL(t *testing.T) {
	base := Config{Brokers: []string{"b:9092"}}
	cases := []struct {
		name string
		mut  func(*Config)
		want error
	}{
		{"user without password", func(c *Config) { c.User = "u"; c.UseTLS = true }, ErrPartialCredentials},
		{"password without user", func(c *Config) { c.Password = "p"; c.UseTLS = true }, ErrPartialCredentials},
		{"unknown mechanism", func(c *Config) { c.User, c.Password, c.UseTLS, c.SASLMechanism = "u", "p", true, "GSSAPI" }, ErrUnknownMechanism},
		{"plain without tls", func(c *Config) { c.User, c.Password = "u", "p" }, ErrPlaintextSASL},
	}
	for _, c := range cases {
		cfg := base
		c.mut(&cfg)
		_, err := New(cfg)
		assert.ErrorIs(t, err, c.want, c.name)
		if err != nil {
			assert.NotContains(t, err.Error(), "p\"", c.name)
		}
	}
}

func TestNew_SCRAMWithoutTLSIsAllowed(t *testing.T) {
	b, err := New(Config{Brokers: []string{"b:9092"}, User: "u", Password: "p", SASLMechanism: "scram-sha-512"})
	require.NoError(t, err, "SCRAM never sends the password")
	assert.Equal(t, sarama.SASLMechanism(sarama.SASLTypeSCRAMSHA512), b.config.Net.SASL.Mechanism)
}

func TestNew_DurableProducerByDefault(t *testing.T) {
	b, err := New(Config{Brokers: []string{"b:9092"}})
	require.NoError(t, err)
	assert.Equal(t, sarama.WaitForAll, b.config.Producer.RequiredAcks)
	assert.True(t, b.config.Producer.Idempotent)
	assert.Equal(t, 1, b.config.Net.MaxOpenRequests)

	b, err = New(Config{Brokers: []string{"b:9092"}, Acks: "leader"})
	require.NoError(t, err)
	assert.Equal(t, sarama.WaitForLocal, b.config.Producer.RequiredAcks)
	assert.False(t, b.config.Producer.Idempotent)

	_, err = New(Config{Brokers: []string{"b:9092"}, Acks: "most"})
	assert.Error(t, err)
}

func TestNew_CustomTLSIsRaisedToTLS12(t *testing.T) {
	custom := &tls.Config{MinVersion: tls.VersionTLS10, ServerName: "kafka.internal"} // #nosec G402 -- verifies the raise to TLS 1.2
	b, err := New(Config{Brokers: []string{"b:9093"}, TLS: custom})
	require.NoError(t, err)
	assert.True(t, b.config.Net.TLS.Enable)
	assert.Equal(t, uint16(tls.VersionTLS12), b.config.Net.TLS.Config.MinVersion)
	assert.Equal(t, "kafka.internal", b.config.Net.TLS.Config.ServerName)
	assert.Equal(t, uint16(tls.VersionTLS10), custom.MinVersion, "the caller's config is not mutated")
}

func TestConfig_RedactsPassword(t *testing.T) {
	cfg := Config{Brokers: []string{"b:9092"}, User: "u", Password: "s3cret"}
	js, _ := json.Marshal(cfg)
	for _, out := range []string{fmt.Sprintf("%v", cfg), fmt.Sprintf("%+v", cfg), fmt.Sprintf("%#v", cfg), string(js), cfg.LogValue().String()} {
		assert.NotContains(t, out, "s3cret")
		assert.True(t, strings.Contains(out, "REDACTED"), out)
	}
}

func TestDecode_StableIDForForeignRecords(t *testing.T) {
	sm := &sarama.ConsumerMessage{Topic: "orders", Partition: 3, Offset: 42, Value: []byte(`"v"`)}
	a, b := decode(sm), decode(sm)
	assert.Equal(t, a.Id, b.Id, "a redelivered record keeps its Id")
	other := *sm
	other.Offset = 43
	assert.NotEqual(t, a.Id, decode(&other).Id)
}
