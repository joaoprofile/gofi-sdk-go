// Package bucketenv opens a bucket.Store from the environment configuration.
//
// It is the single place that imports every backend, so the core bucket package
// and the backends stay decoupled from the environment loader. The mapping from
// environment.BucketConfig to each backend's typed Config is direct and
// compile-time checked — no string-keyed indirection.
package bucketenv

import (
	"fmt"

	"github.com/joaoprofile/gofi/base/bucket"
	"github.com/joaoprofile/gofi/base/bucket/minio"
	"github.com/joaoprofile/gofi/base/bucket/oci"
	"github.com/joaoprofile/gofi/base/environment"
)

// Open builds a Store from the given BucketConfig, dispatching on the provider.
// It returns ErrInvalidConfig wrapping the offending provider when unset or
// unsupported.
func Open(bc environment.BucketConfig) (bucket.Store, error) {
	switch bc.Provider {
	case environment.BUCKET_OCI:
		c := bc.OCICredentials
		return oci.New(oci.Config{
			Bucket:      bc.Name,
			Region:      bc.Region,
			Endpoint:    bc.Endpoint,
			Namespace:   c.Namespace,
			TenancyID:   c.TenancyID,
			UserID:      c.UserID,
			Fingerprint: c.FingerPrint,
			PrivateKey:  c.PrivateKey,
			Passphrase:  c.Passphrase,
		})
	case environment.BUCKET_MINIO:
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

// OpenFromEnv builds a Store from the process environment singleton.
func OpenFromEnv() (bucket.Store, error) {
	return Open(environment.Instance().Bucket())
}
