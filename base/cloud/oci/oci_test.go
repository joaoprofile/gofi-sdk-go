package oci

import (
	"errors"
	"testing"
)

func TestConfigurationProvider_APIKeyValidation(t *testing.T) {
	full := Config{Region: "us-ashburn-1", TenancyID: "t", UserID: "u", Fingerprint: "f", PrivateKey: "k"}
	if _, err := ConfigurationProvider(full); err != nil {
		t.Fatalf("complete api key config: %v", err)
	}
	for name, mutate := range map[string]func(*Config){
		"tenancy": func(c *Config) { c.TenancyID = "" },
		"user":    func(c *Config) { c.UserID = "" },
		"finger":  func(c *Config) { c.Fingerprint = "" },
		"key":     func(c *Config) { c.PrivateKey = "" },
		"region":  func(c *Config) { c.Region = "" },
	} {
		c := full
		mutate(&c)
		if _, err := ConfigurationProvider(c); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s missing: err=%v, want ErrInvalidConfig", name, err)
		}
	}
}

func TestConfigurationProvider_UnknownMode(t *testing.T) {
	if _, err := ConfigurationProvider(Config{AuthMode: "magic"}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err=%v", err)
	}
}
