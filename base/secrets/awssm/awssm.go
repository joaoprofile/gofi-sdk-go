// Package awssm resolves secret://awssm/<name-or-arn>[#key] from AWS Secrets
// Manager. Importing it registers the provider; identity and region come from
// the AWS default chain (AWS_REGION, IRSA / EKS Pod Identity, roles).
package awssm

import (
	"context"
	"errors"
	"fmt"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	cloudaws "github.com/joaoprofile/gofi-sdk-go/base/cloud/aws"
	"github.com/joaoprofile/gofi-sdk-go/base/secrets"
)

// Provider is the reference provider name.
const Provider = "awssm"

func init() {
	secrets.Register(Provider, func(ctx context.Context) (secrets.Store, error) {
		return New(ctx, cloudaws.Config{})
	})
}

type api interface {
	GetSecretValue(ctx context.Context, in *secretsmanager.GetSecretValueInput, opts ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

// Store reads secrets from AWS Secrets Manager.
type Store struct{ client api }

// New resolves the AWS identity for cfg (zero value: default chain).
func New(ctx context.Context, cfg cloudaws.Config) (*Store, error) {
	awsCfg, err := cloudaws.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return NewWithConfig(awsCfg), nil
}

// NewWithConfig builds a Store from an existing aws.Config.
func NewWithConfig(awsCfg awssdk.Config) *Store {
	return &Store{client: secretsmanager.NewFromConfig(awsCfg)}
}

// Get returns the current version of the secret (string or binary).
func (s *Store) Get(ctx context.Context, name string) (string, error) {
	out, err := s.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: &name})
	if err != nil {
		if _, ok := errors.AsType[*smtypes.ResourceNotFoundException](err); ok {
			return "", fmt.Errorf("%w: %w", secrets.ErrNotFound, err)
		}
		return "", err
	}
	if out.SecretString != nil {
		return *out.SecretString, nil
	}
	return string(out.SecretBinary), nil
}
