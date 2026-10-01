package aws

import (
	"context"
	"testing"
)

func TestLoad_StaticCredentialsAndEndpoint(t *testing.T) {
	t.Setenv("AWS_CONFIG_FILE", "/nonexistent")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/nonexistent")
	cfg, err := Load(context.Background(), Config{
		Region: "sa-east-1", Endpoint: "http://localhost:4566",
		AccessKeyID: "AKID", SecretAccessKey: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	creds, err := cfg.Credentials.Retrieve(context.Background())
	if err != nil || creds.AccessKeyID != "AKID" {
		t.Fatalf("static credentials not applied: %+v %v", creds, err)
	}
	if cfg.Region != "sa-east-1" || cfg.BaseEndpoint == nil || *cfg.BaseEndpoint != "http://localhost:4566" {
		t.Fatalf("region/endpoint not applied: %s %v", cfg.Region, cfg.BaseEndpoint)
	}
}

func TestLoad_DefaultChainFromEnvironment(t *testing.T) {
	t.Setenv("AWS_CONFIG_FILE", "/nonexistent")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/nonexistent")
	t.Setenv("AWS_ACCESS_KEY_ID", "ENVKEY")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "envsecret")
	cfg, err := Load(context.Background(), Config{Region: "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	creds, err := cfg.Credentials.Retrieve(context.Background())
	if err != nil || creds.AccessKeyID != "ENVKEY" {
		t.Fatalf("default chain must pick env credentials: %+v %v", creds, err)
	}
}
