package config

import (
	"github.com/joaoprofile/gofi-sdk-go/base/bucket"
	"github.com/joaoprofile/gofi-sdk-go/base/environment"
)

// Bucket builds a bucket.Config from the BUCKET_* variables of the environment.
// Open it with bucket.Open after importing the provider package
// (base/bucket/s3 or base/bucket/oci).
func Bucket(env *environment.Environment) bucket.Config {
	return bucket.Config{
		Provider: bucket.Provider(env.BucketProvider),
		Name:     env.BucketName,
		Region:   env.BucketRegion,
		Endpoint: env.BucketEndpoint,
		// BUCKET_PRESIGN_MAX_TTL only lowers the 7-day cap.
		PresignMaxTTL: env.BucketPresignMaxTTL,
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
