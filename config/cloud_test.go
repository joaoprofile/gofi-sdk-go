package config

import (
	"testing"

	"github.com/joaoprofile/gofi/base/cloud"
	"github.com/joaoprofile/gofi/base/environment"
)

func TestCloud_MapsEnv(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	t.Setenv("CLOUD_PROVIDER", "aws")
	t.Setenv("CLOUD_REGION", "us-east-1")
	t.Setenv("CLOUD_HOST", "localhost:4566")
	t.Setenv("CLOUD_TOKEN", "tok")
	t.Setenv("CLOUD_SECRET", "sec")
	t.Setenv("CLOUD_DISABLE_SSL", "true")

	cfg := Cloud(environment.Instance())
	if cfg.Provider != cloud.ProviderAWS {
		t.Errorf("Provider=%q, want aws", cfg.Provider)
	}
	if cfg.Region != "us-east-1" || cfg.Host != "localhost:4566" {
		t.Errorf("region/host not mapped: %+v", cfg)
	}
	if cfg.Token != "tok" || cfg.Secret != "sec" || !cfg.DisableSSL {
		t.Errorf("creds/ssl not mapped: %+v", cfg)
	}
}

func TestInitCloud_NoProvider_ReturnsError(t *testing.T) {
	cloud.ResetForTesting()
	t.Cleanup(cloud.ResetForTesting)
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	t.Setenv("CLOUD_PROVIDER", "none")

	if _, err := InitCloud(environment.Instance()); err == nil {
		t.Fatal("expected error for 'none' provider")
	}
}
