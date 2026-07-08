package oci

import (
	"testing"

	"github.com/joaoprofile/gofi/base/bucket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testPrivateKey is a throwaway RSA key in PEM form, generated solely so the
// SDK configuration provider can parse it during client construction. It is NOT
// a real credential and grants access to nothing.
const testPrivateKey = `-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQDoXU6mSsUStp1B
7Dsa7y/13coeu9Nd4Zu1Y80+1/0WhPE4nQdeRR6cy8C3F/BGZSSga8Np0tF6v+ea
R+TfAG2BlZrDiKp8dTvPWwln6MSvgRg6kexlc8hn1opdtxhEkZ1d4D69pLl7ydtF
b2DcvWG9KFZDA6Brzu94aIcPnQ/FtXUW5Pdp66i+mQFr0nb54n6KtYsVCXybuQkq
hnew+iFcRkMN3DHftwVkW8jRzJ/zCTR0vqH+wRycx6eB3BDBtBqBCyejBD1VA9G+
HtqhALH5WDpLGP/eAbtPbVWH8t1ykFTOlnrS2mzjjy5amk7m9XNCLU5Bsb5bHL/J
ckLrpvRXAgMBAAECggEAb/jOtqmfL/ZZ73ODw+XxCZzYEllWcI4QN6ehNyBj8F8d
0rcw3seWCd7RvilF+tYwgTGM2Ejj8y/YzmrIqoGNQ32xN3p7FUB1EuX+sVjktuIR
p9+7t+PEde1XffOGOTymRZ+S/FYNn85U4K/cUGLeX4W5k8+ClZEBqtdMBkUcXZu5
b9WeG8ibYHMJcXQ2PZSSt2uS5ZZQuV8kqJKGReCOP2eJHnSip9Mm9x7HPo/DX++r
c5/tB8iKswFNvkibZqRNdnClt4zA9Q5DKwWV/YMbrfdlQ3QEYx+fkLqwS7LmbhFv
8fW0MlKkHbm7trsJZ/+omHmdu4xMWElCuYAmEXRLuQKBgQD5/bM2bmLm02WqAN9k
GTAwQJtPr9hE4BMSRkE4SldVK6MWHqIUnP52sC5Y2dA5jdyFKs+XTSLMVXVcIE39
Z237jzfNDvhLEkGzleqsuaru6qSlLsBb/TUdAUznA2tvXWjOFT2hxScsi59rxI0L
+A5kNXV4BoAD48prHVnOpEHULQKBgQDt8yTI03fGGd9SfDn7CgwkzWMcm1sTRizZ
sARPhVGtr9lLxTLbwE2EfviKt1j26fX7OYgvjgcfP4wy2vELx4wNPBv/8SRRQARB
BO2K1Oad+rYNj6M53WksvlBINHaNTsgyrSReESN2CR0aL3h68kQe3RnWoIl2RcOM
kvnCf40pEwKBgQD5OWvJABOpe2cHLQeIi3P3JvGvZ+d8AsgAl/m9XJ/kUTStgKyl
UD5/pPUPr1ZfioYmXJ/IfyYJ/8iYp7wYvVxwRj+jNyFh9jl6CCOFPzSiK1spMoqj
KrQgzoMUa9xXkhBCI/rlo9+CEVBF6BWVsR7n2EPb/N7zAc1zLDe0Qx09oQKBgQCr
Cr0lUsTk9JIQE9YFuxoxliWpWY8lEquIqzreAoJM7HuxOIYvalMOa8qyw8rCajj0
Jk3biSdbce2QXMsqYX0twkiKOMeXVAH6ztUFl7ZSVvZoVxjIrnw8umyxCm0xdDD6
JHVg3Mb1wOVXfkoHboBDA0HggrNs/gbr1HaONeu9WwKBgA/cz2qTYVgpPdC6E6Ky
bzGEdoF3nanaTdT9cesG43OMcwOmxtInZn/9sCSJ6nZeBwheicsWFn8HrHtIgH81
FgT57ucLIFu3MF/Jt89sUvnfD+B451nLLwvmOyN24QRD3TSaxZUJewnikIfyA58T
MbRdPvVxXn517zTdQa05Wlku
-----END PRIVATE KEY-----`

func validConfig() Config {
	return Config{
		Bucket:      "my-bucket",
		Region:      "sa-saopaulo-1",
		TenancyID:   "ocid1.tenancy.oc1..aaaa",
		UserID:      "ocid1.user.oc1..bbbb",
		Fingerprint: "aa:bb:cc",
		PrivateKey:  testPrivateKey,
	}
}

func TestNew_MissingFields_ReturnsInvalidConfig(t *testing.T) {
	cases := map[string]func(c *Config){
		"bucket":      func(c *Config) { c.Bucket = "" },
		"tenancy":     func(c *Config) { c.TenancyID = "" },
		"user":        func(c *Config) { c.UserID = "" },
		"region":      func(c *Config) { c.Region = "" },
		"fingerprint": func(c *Config) { c.Fingerprint = "" },
		"privateKey":  func(c *Config) { c.PrivateKey = "" },
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
	cfg.AuthMode = "bogus"
	_, err := New(cfg)
	require.Error(t, err)
	assert.ErrorIs(t, err, bucket.ErrInvalidConfig)
}

func TestNew_SatisfiesStoreInterface(t *testing.T) {
	s, err := New(validConfig())
	require.NoError(t, err)
	var _ bucket.Store = s
}
