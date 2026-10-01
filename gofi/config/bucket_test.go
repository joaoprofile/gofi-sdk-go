package config

import (
	"testing"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/base/bucket"
	"github.com/joaoprofile/gofi-sdk-go/base/environment"
)

func TestBucket_MapsEnv(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	t.Setenv("BUCKET_PROVIDER", "oci")
	t.Setenv("BUCKET_NAME", "my-bucket")
	t.Setenv("BUCKET_REGION", "sa-saopaulo-1")
	t.Setenv("BUCKET_ENDPOINT", "objectstorage.example.com")
	t.Setenv("BUCKET_OCI_AUTH_MODE", "instance_principal")
	t.Setenv("BUCKET_OCI_TENANCY_ID", "tenancy")
	t.Setenv("BUCKET_OCI_USER_ID", "user")
	t.Setenv("BUCKET_S3_ACCESS_KEY", "ak")
	t.Setenv("BUCKET_S3_USE_SSL", "true")
	t.Setenv("BUCKET_PRESIGN_MAX_TTL", "15m")

	cfg := Bucket(environment.Instance())
	if cfg.Provider != bucket.ProviderOCI {
		t.Errorf("Provider=%q, want oci", cfg.Provider)
	}
	if cfg.Name != "my-bucket" || cfg.Region != "sa-saopaulo-1" {
		t.Errorf("name/region not mapped: %+v", cfg)
	}
	if cfg.PresignMaxTTL != 15*time.Minute {
		t.Errorf("PresignMaxTTL=%s, want 15m", cfg.PresignMaxTTL)
	}
	if cfg.Endpoint != "objectstorage.example.com" {
		t.Errorf("Endpoint=%q", cfg.Endpoint)
	}
	if cfg.OCICredentials.TenancyID != "tenancy" || cfg.OCICredentials.UserID != "user" {
		t.Errorf("oci creds not mapped: %+v", cfg.OCICredentials)
	}
	if cfg.OCICredentials.AuthMode != bucket.OCIAuthInstancePrincipal {
		t.Errorf("AuthMode=%q, want instance_principal", cfg.OCICredentials.AuthMode)
	}
	if cfg.S3Credentials.AccessKey != "ak" || !cfg.S3Credentials.UseSSL {
		t.Errorf("s3 creds not mapped: %+v", cfg.S3Credentials)
	}
	if !cfg.IsConfigured() {
		t.Error("expected IsConfigured() true for oci provider")
	}
}

func TestBucketConfig_IsConfigured(t *testing.T) {
	if (bucket.Config{}).IsConfigured() {
		t.Error("empty provider must report not configured")
	}
	if (bucket.Config{Provider: bucket.ProviderNone}).IsConfigured() {
		t.Error("'none' provider must report not configured")
	}
	if !(bucket.Config{Provider: bucket.ProviderMinIO}).IsConfigured() {
		t.Error("'minio' provider must report configured")
	}
}
