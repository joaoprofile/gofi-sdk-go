package bucket

import (
	"errors"
	"testing"
)

func TestParseURL(t *testing.T) {
	cfg, err := ParseURL("s3://docs?region=us-east-1&endpoint=minio:9000&ssl=false")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != ProviderS3 || cfg.Name != "docs" || cfg.Region != "us-east-1" || cfg.Endpoint != "minio:9000" || cfg.S3Credentials.UseSSL {
		t.Errorf("s3: %+v", cfg)
	}
	cfg, _ = ParseURL("oci://docs?region=sa-saopaulo-1&namespace=ns&auth=workload_identity")
	if cfg.Provider != ProviderOCI || cfg.OCICredentials.Namespace != "ns" || cfg.OCICredentials.AuthMode != OCIAuthWorkloadIdentity {
		t.Errorf("oci: %+v", cfg)
	}
	cfg, _ = ParseURL("file:///var/data")
	if cfg.Provider != ProviderFile || cfg.Endpoint != "/var/data" || cfg.Name != "" {
		t.Errorf("file: %+v", cfg)
	}
	if _, err := ParseURL("no-scheme"); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("missing scheme: %v", err)
	}
}
