// Package bucket defines a provider-agnostic object-storage abstraction.
//
// A Store models a single bucket and exposes the minimal set of operations
// shared by every object-storage backend: upload, download, list and delete.
// Concrete backends (OCI Object Storage, AWS S3, GCS, …) live in their own
// sub-packages and register themselves through Register so that callers can
// obtain a Store by provider name via Open, without importing the backend
// directly.
package bucket

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// Sentinel errors returned by every Store implementation so that callers can
// branch on the failure mode independently of the underlying provider.
var (
	// ErrNotFound is returned when an object (or its bucket) does not exist.
	ErrNotFound = errors.New("bucket: object not found")

	// ErrInvalidConfig is returned by a factory when the supplied Config is
	// missing required fields.
	ErrInvalidConfig = errors.New("bucket: invalid configuration")

	// ErrInvalidTTL is returned by PresignGet when ttl is not positive or
	// exceeds the store's limit.
	ErrInvalidTTL = errors.New("bucket: invalid presign ttl")

	// ErrBucketNotFound is returned when the bucket itself does not exist. It
	// wraps ErrNotFound, so errors.Is(err, ErrNotFound) still holds.
	ErrBucketNotFound = fmt.Errorf("%w: bucket does not exist", ErrNotFound)

	// ErrBucketExists is returned by CreateBucket when the name is taken.
	ErrBucketExists = errors.New("bucket: bucket already exists")

	// ErrBucketNotEmpty is returned by DeleteBucket without Force when the
	// bucket still holds objects.
	ErrBucketNotEmpty = errors.New("bucket: bucket is not empty")

	// ErrInvalidBucketName is returned when a name breaks the provider's rules.
	ErrInvalidBucketName = errors.New("bucket: invalid bucket name")

	// ErrAccessDenied is returned when the credentials lack permission.
	ErrAccessDenied = errors.New("bucket: access denied")

	// ErrNotSupported is returned when a provider does not implement a feature.
	ErrNotSupported = errors.New("bucket: not supported by provider")
)

// MaxPresignTTL is the longest validity PresignGet grants on any backend: the
// S3 SigV4 limit. OCI PARs cannot be revoked by the URL holder, so the same
// cap bounds how long a leaked URL stays usable.
const MaxPresignTTL = 7 * 24 * time.Hour

// PresignLimit returns the effective cap for limit: MaxPresignTTL when limit
// is not positive or larger than it.
func PresignLimit(limit time.Duration) time.Duration {
	if limit <= 0 || limit > MaxPresignTTL {
		return MaxPresignTTL
	}
	return limit
}

// CheckPresignTTL returns ErrInvalidTTL unless 0 < ttl <= PresignLimit(limit).
func CheckPresignTTL(ttl, limit time.Duration) error {
	if ceiling := PresignLimit(limit); ttl <= 0 || ttl > ceiling {
		return fmt.Errorf("%w: %s must be in (0, %s]", ErrInvalidTTL, ttl, ceiling)
	}
	return nil
}

// Object is the provider-agnostic metadata of a stored object.
type Object struct {
	// Key is the object name, unique within the bucket.
	Key string
	// Size is the object size in bytes. It may be zero when the backend does
	// not report it in a listing.
	Size int64
	// ContentType is the MIME type, when known.
	ContentType string
	// LastModified is the object's creation/modification time, when known.
	LastModified time.Time
}

// PutInput describes an upload.
type PutInput struct {
	// Key is the destination object name. Required.
	Key string
	// Body streams the object contents. Required.
	Body io.Reader
	// Size is the content length in bytes. Set it to a negative value when the
	// size is unknown; backends that require it will buffer or stream
	// accordingly.
	Size int64
	// ContentType is the optional MIME type to store alongside the object.
	ContentType string
}

// Store is the behaviour every object-storage backend must implement. All
// methods are safe for concurrent use.
type Store interface {
	// Put uploads an object, overwriting any existing object with the same key.
	Put(ctx context.Context, in PutInput) error

	// Get downloads an object. The caller owns the returned ReadCloser and must
	// close it. It returns ErrNotFound when the key does not exist.
	Get(ctx context.Context, key string) (Object, io.ReadCloser, error)

	// List returns the objects whose key starts with prefix. An empty prefix
	// lists the whole bucket. The returned slice is never nil.
	List(ctx context.Context, prefix string) ([]Object, error)

	// Delete removes an object. It is idempotent: deleting a key that does not
	// exist is not an error.
	Delete(ctx context.Context, key string) error

	// PresignGet returns a time-limited, read-only URL for a single object that
	// a client can download directly from the backend, without proxying the
	// bytes through the application. ttl bounds the URL's validity and must be
	// in (0, limit], where limit is Config.PresignMaxTTL capped at
	// MaxPresignTTL; otherwise it returns ErrInvalidTTL. OCI issues a
	// Pre-Authenticated Request; S3/MinIO a presigned GET.
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
}
