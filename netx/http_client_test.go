package netx

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewClient_WithValidConfig(t *testing.T) {
	name := "TestClient"
	baseURL := "https://gofi.com"

	config := &HttpClientConfig{
		Name:       name,
		BaseURL:    baseURL,
		Timeout:    10 * time.Second,
		Retries:    5,
		RetrySleep: 1 * time.Second,
		RateLimit:  15,
	}

	client, err := NewClient(config)

	assert.NoError(t, err)

	assert.Equal(t, name, client.Name)
	assert.Equal(t, baseURL, client.BaseURL)
	assert.Equal(t, uint8(5), client.Retries)
	assert.Equal(t, 15, client.RateLimit)
}

func TestNewClient_WithNilConfig(t *testing.T) {
	client, err := NewClient(nil)

	assert.Nil(t, client)
	assert.EqualError(t, err, ErrConfigNotProvided)
}

func TestNewClient_DefaultTimeout(t *testing.T) {
	config := &HttpClientConfig{
		Name:    "TimeoutClient",
		BaseURL: "https://gofi.com",
	}
	client, err := NewClient(config)
	assert.NoError(t, err)
	assert.Equal(t, defaultTimeout, client.Client.Timeout)
}

func TestNewClient_WithDefaults(t *testing.T) {
	config := &HttpClientConfig{
		Name:    "DefaultClient",
		BaseURL: "https://gofi.com",
	}

	client, err := NewClient(config)

	assert.NoError(t, err)

	assert.Equal(t, "DefaultClient", client.Name)
	assert.Equal(t, uint8(5), client.Retries)
	assert.Equal(t, 10, client.RateLimit)
	assert.Equal(t, 2*time.Second, client.RetrySleep)
}
