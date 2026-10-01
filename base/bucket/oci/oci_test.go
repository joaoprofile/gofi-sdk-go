package oci

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/base/bucket"
	cloudoci "github.com/joaoprofile/gofi-sdk-go/base/cloud/oci"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testPrivateKey is a throwaway RSA key generated per test run, so the SDK
// configuration provider can parse it during client construction.
var testPrivateKey = func() string {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)}))
}()

func validConfig() Config {
	return Config{
		Bucket: "my-bucket",
		Credentials: cloudoci.Config{
			Region:      "sa-saopaulo-1",
			TenancyID:   "ocid1.tenancy.oc1..aaaa",
			UserID:      "ocid1.user.oc1..bbbb",
			Fingerprint: "aa:bb:cc",
			PrivateKey:  testPrivateKey,
		},
	}
}

func TestNew_MissingFields_ReturnsInvalidConfig(t *testing.T) {
	cases := map[string]func(c *Config){
		"bucket":      func(c *Config) { c.Bucket = "" },
		"tenancy":     func(c *Config) { c.Credentials.TenancyID = "" },
		"user":        func(c *Config) { c.Credentials.UserID = "" },
		"region":      func(c *Config) { c.Credentials.Region = "" },
		"fingerprint": func(c *Config) { c.Credentials.Fingerprint = "" },
		"privateKey":  func(c *Config) { c.Credentials.PrivateKey = "" },
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

func TestNew_PreSeededNamespace_SkipsLookup(t *testing.T) {
	cfg := validConfig()
	cfg.Namespace = "my-namespace"
	s, err := New(cfg)
	require.NoError(t, err)
	require.NotNil(t, s)

	// resolveNamespace must return the pre-seeded value without any network call.
	ns, err := s.resolveNamespace(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "my-namespace", ns)
}

func TestNew_UnsupportedAuthMode_ReturnsInvalidConfig(t *testing.T) {
	cfg := validConfig()
	cfg.Credentials.AuthMode = "bogus"
	_, err := New(cfg)
	require.Error(t, err)
	assert.ErrorIs(t, err, bucket.ErrInvalidConfig)
}

func TestNew_SatisfiesStoreInterface(t *testing.T) {
	s, err := New(validConfig())
	require.NoError(t, err)
	var _ bucket.Store = s
}

// Regression: PARs cannot be revoked by the URL holder, so PresignGet must
// refuse a non-positive ttl or one above the configured cap before creating one.
func TestPresignGet_RejectsTTLOutsideLimit(t *testing.T) {
	cfg := validConfig()
	cfg.Namespace = "ns"
	cfg.PresignMaxTTL = time.Hour
	s, err := New(cfg)
	require.NoError(t, err)
	for _, ttl := range []time.Duration{0, -time.Second, time.Hour + time.Second} {
		_, err := s.PresignGet(context.Background(), "a.txt", ttl)
		assert.ErrorIs(t, err, bucket.ErrInvalidTTL, "ttl %s", ttl)
	}
}

func TestOpen_MapsPresignMaxTTL(t *testing.T) {
	c := validConfig().Credentials
	st, err := bucket.Open(context.Background(), bucket.Config{
		Provider: bucket.ProviderOCI, Name: "b", Region: c.Region, PresignMaxTTL: time.Hour,
		OCICredentials: bucket.OCICredentials{Namespace: "ns", TenancyID: c.TenancyID, UserID: c.UserID,
			FingerPrint: c.Fingerprint, PrivateKey: c.PrivateKey},
	})
	require.NoError(t, err)
	assert.Equal(t, time.Hour, st.(*Store).presignMax)
}
