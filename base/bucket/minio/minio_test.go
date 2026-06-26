package minio

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/joaoprofile/gofi/base/bucket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validConfig() Config {
	return Config{
		Bucket:    "my-bucket",
		Endpoint:  "localhost:9000",
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
	}
}

func TestNew_MissingFields_ReturnsInvalidConfig(t *testing.T) {
	cases := map[string]func(c *Config){
		"bucket":    func(c *Config) { c.Bucket = "" },
		"endpoint":  func(c *Config) { c.Endpoint = "" },
		"accessKey": func(c *Config) { c.AccessKey = "" },
		"secretKey": func(c *Config) { c.SecretKey = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := validConfig()
			mutate(&cfg)
			_, err := New(cfg)
			require.Error(t, err)
			assert.ErrorIs(t, err, bucket.ErrInvalidConfig)
		})
	}
}

func TestNew_Success_SatisfiesStoreInterface(t *testing.T) {
	s, err := New(validConfig())
	require.NoError(t, err)
	require.NotNil(t, s)
	var _ bucket.Store = s
}

func TestToReadSeeker_PassesThroughSeeker(t *testing.T) {
	in := bytes.NewReader([]byte("hello"))
	got, err := toReadSeeker(in)
	require.NoError(t, err)
	assert.Same(t, in, got, "an existing ReadSeeker must be returned as-is")
}

func TestToReadSeeker_BuffersNonSeeker(t *testing.T) {
	// strings.NewReader is a ReadSeeker, so wrap it to hide the Seek method.
	in := io.NopCloser(strings.NewReader("hello"))
	got, err := toReadSeeker(in)
	require.NoError(t, err)

	data, err := io.ReadAll(got)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))
}
