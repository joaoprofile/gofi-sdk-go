package config

import (
	"fmt"

	"github.com/joaoprofile/gofi/base/bucket"
	"github.com/joaoprofile/gofi/base/bucket/minio"
	"github.com/joaoprofile/gofi/base/bucket/oci"
	"github.com/joaoprofile/gofi/base/environment"
)

// Bucket builds a bucket.Config from the BUCKET_* variables of the environment.
func Bucket(env *environment.Environment) bucket.Config {
	return bucket.Config{
		Provider: bucket.Provider(env.BucketProvider),
		Name:     env.BucketName,
		Region:   env.BucketRegion,
		Endpoint: env.BucketEndpoint,
		OCICredentials: bucket.OCICredentials{
			AuthMode:    bucket.OCIAuthMode(env.BucketOCIAuthMode),
			Namespace:   env.BucketOCINamespace,
			TenancyID:   env.BucketOCITenancyID,
			UserID:      env.BucketOCIUserID,
			FingerPrint: env.BucketOCIFingerPrint,
			PrivateKey:  env.BucketOCIPrivateKey,
			Passphrase:  env.BucketOCIPassphrase,
		},
		S3Credentials: bucket.S3Credentials{
			AccessKey: env.BucketS3AccessKey,
			SecretKey: env.BucketS3SecretKey,
			UseSSL:    env.BucketS3UseSSL,
		},
	}
}

// OpenBucket builds a bucket.Store from a bucket.Config, dispatching on the
// provider. This is the single place that imports every backend, keeping the
// core bucket package and the backends decoupled. It returns
// bucket.ErrInvalidConfig wrapping the offending provider when unset or
// unsupported.
func OpenBucket(bc bucket.Config) (bucket.Store, error) {
	switch bc.Provider {
	case bucket.ProviderOCI:
		c := bc.OCICredentials
		return oci.New(oci.Config{
			Bucket:      bc.Name,
			Region:      bc.Region,
			Endpoint:    bc.Endpoint,
			AuthMode:    c.AuthMode,
			Namespace:   c.Namespace,
			TenancyID:   c.TenancyID,
			UserID:      c.UserID,
			Fingerprint: c.FingerPrint,
			PrivateKey:  c.PrivateKey,
			Passphrase:  c.Passphrase,
		})
	case bucket.ProviderMinIO:
		c := bc.S3Credentials
		return minio.New(minio.Config{
			Bucket:    bc.Name,
			Endpoint:  bc.Endpoint,
			Region:    bc.Region,
			AccessKey: c.AccessKey,
			SecretKey: c.SecretKey,
			UseSSL:    c.UseSSL,
		})
	default:
		return nil, fmt.Errorf("%w: unsupported bucket provider %q", bucket.ErrInvalidConfig, bc.Provider)
	}
}

// OpenBucketFromEnv builds a bucket.Store from the process environment.
func OpenBucketFromEnv(env *environment.Environment) (bucket.Store, error) {
	return OpenBucket(Bucket(env))
}
