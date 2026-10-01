// Package aws loads AWS credentials for every gofi integration (S3, SQS, RDS,
// SigV4 signing). Without explicit keys it uses the default credential chain:
// environment, shared config, IRSA / EKS Pod Identity, ECS and EC2 roles.
package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

// Config holds optional overrides; the zero value uses the default chain.
type Config struct {
	Region string
	// Endpoint overrides the service endpoint (LocalStack, MinIO, R2, ...).
	Endpoint string
	// Static credentials; leave empty to use the default credential chain.
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	// Profile selects a shared-config profile.
	Profile string
}

// mask hides a secret value: "[REDACTED]" when set, "" when empty.
func mask(s string) string {
	if s == "" {
		return ""
	}
	return "[REDACTED]"
}

// plain drops Config's methods so the masked copy renders with the defaults.
type plain Config

func (c Config) redacted() plain {
	c.SecretAccessKey = mask(c.SecretAccessKey)
	c.SessionToken = mask(c.SessionToken)
	return plain(c)
}

// String, GoString, Format, LogValue and MarshalJSON mask the secret fields.
func (c Config) String() string { return fmt.Sprintf("%+v", c.redacted()) }

func (c Config) GoString() string {
	r := c.redacted()
	return fmt.Sprintf("aws.Config{Region:%#v, Endpoint:%#v, AccessKeyID:%#v, SecretAccessKey:%#v, SessionToken:%#v, Profile:%#v}", r.Region, r.Endpoint, r.AccessKeyID, r.SecretAccessKey, r.SessionToken, r.Profile)
}

func (c Config) Format(f fmt.State, verb rune) {
	if verb == 'v' && f.Flag('#') {
		_, _ = f.Write([]byte(c.GoString()))
		return
	}
	format := "%v"
	if f.Flag('+') {
		format = "%+v"
	}
	_, _ = fmt.Fprintf(f, format, c.redacted())
}

func (c Config) LogValue() slog.Value {
	r := c.redacted()
	return slog.GroupValue(
		slog.Any("Region", r.Region),
		slog.Any("Endpoint", r.Endpoint),
		slog.Any("AccessKeyID", r.AccessKeyID),
		slog.Any("SecretAccessKey", r.SecretAccessKey),
		slog.Any("SessionToken", r.SessionToken),
		slog.Any("Profile", r.Profile),
	)
}

func (c Config) MarshalJSON() ([]byte, error) { return json.Marshal(c.redacted()) }

// Load resolves an aws.Config for cfg.
func Load(ctx context.Context, cfg Config) (awssdk.Config, error) {
	var opts []func(*config.LoadOptions) error
	if cfg.Region != "" {
		opts = append(opts, config.WithRegion(cfg.Region))
	}
	if cfg.Profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(cfg.Profile))
	}
	if cfg.AccessKeyID != "" || cfg.SecretAccessKey != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken)))
	}
	awsCfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return awssdk.Config{}, fmt.Errorf("aws: load config: %w", err)
	}
	if cfg.Endpoint != "" {
		awsCfg.BaseEndpoint = awssdk.String(cfg.Endpoint)
	}
	return awsCfg, nil
}
