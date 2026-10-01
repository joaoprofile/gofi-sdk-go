package bucket

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// ParseURL builds a Config from a bucket URL; credentials still come from
// the provider's default chain or Config fields. Examples:
//
//	s3://my-bucket?region=us-east-1
//	s3://my-bucket?endpoint=minio:9000&ssl=false
//	oci://my-bucket?region=sa-saopaulo-1&namespace=ns&auth=workload_identity
//	file:///var/data/uploads
//	mem://test
func ParseURL(raw string) (Config, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Config{}, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	if u.Scheme == "" {
		return Config{}, fmt.Errorf("%w: url %q has no scheme", ErrInvalidConfig, raw)
	}
	q := u.Query()
	cfg := Config{
		Provider: Provider(strings.ToLower(u.Scheme)),
		Name:     u.Host,
		Region:   q.Get("region"),
		Endpoint: q.Get("endpoint"),
		OCICredentials: OCICredentials{
			AuthMode:  OCIAuthMode(q.Get("auth")),
			Namespace: q.Get("namespace"),
		},
		S3Credentials: S3Credentials{UseSSL: q.Get("ssl") != "false"},
	}
	if cfg.Provider == ProviderFile {
		cfg.Name, cfg.Endpoint = "", u.Path
	}
	return cfg, nil
}

// OpenURL parses raw and opens the Store; the provider package must be imported.
func OpenURL(ctx context.Context, raw string) (Store, error) {
	cfg, err := ParseURL(raw)
	if err != nil {
		return nil, err
	}
	return Open(ctx, cfg)
}
