// Package rdsauth signs RDS / Aurora IAM authentication tokens, so databases
// accept the pod's AWS identity instead of a static password:
//
//	awsCfg, _ := cloudaws.Load(ctx, cloudaws.Config{})
//	cfg.Password = rdsauth.Password(awsCfg, "db.xxxx.rds.amazonaws.com:5432", "app")
//
// Tokens last 15 minutes and are signed for every new connection. A token is
// a bearer credential, so the postgres driver refuses Config.Password unless
// the DSN verifies the server: sslmode=verify-full (or verify-ca) with the
// RDS CA bundle as sslrootcert.
package rdsauth

import (
	"context"
	"fmt"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"
)

// Password returns a callback for sqln's connection.Config.Password.
// endpoint is host:port of the instance, cluster or proxy.
func Password(awsCfg awssdk.Config, endpoint, user string) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		token, err := auth.BuildAuthToken(ctx, endpoint, awsCfg.Region, user, awsCfg.Credentials)
		if err != nil {
			return "", fmt.Errorf("rdsauth: %w", err)
		}
		return token, nil
	}
}
