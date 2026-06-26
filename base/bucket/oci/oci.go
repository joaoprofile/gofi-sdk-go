// Package oci implements bucket.Store on top of OCI Object Storage.
//
// Build a Store with New. To select the backend from configuration at runtime,
// use gofi's config.OpenBucket wiring instead of importing this package
// directly.
package oci

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/joaoprofile/gofi/base/bucket"
	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/objectstorage"
)

// Config holds the settings required to reach a single OCI Object Storage
// bucket.
type Config struct {
	// Bucket is the target bucket name. Required.
	Bucket string
	// Region is the OCI region identifier, e.g. "sa-saopaulo-1". Required.
	Region string
	// Namespace is the Object Storage namespace. When empty it is resolved
	// automatically on first use.
	Namespace string
	// Endpoint optionally overrides the region-derived host (dedicated
	// endpoints, emulators).
	Endpoint string

	// API-signing credentials. All required.
	TenancyID   string
	UserID      string
	Fingerprint string
	PrivateKey  string
	// Passphrase is optional; set it only when PrivateKey is encrypted.
	Passphrase string
}

// Store is the OCI Object Storage implementation of bucket.Store. It targets a
// single bucket and resolves the Object Storage namespace lazily on first use.
type Store struct {
	client objectstorage.ObjectStorageClient
	bucket string

	nsOnce    sync.Once
	namespace string
	nsErr     error
}

// compile-time guarantee that *Store satisfies the abstraction.
var _ bucket.Store = (*Store)(nil)

// New builds an OCI-backed Store from cfg. It validates credentials and
// constructs the SDK client but performs no network I/O; the namespace and
// bucket are contacted only when a Store method is called.
func New(cfg Config) (*Store, error) {
	switch {
	case cfg.Bucket == "":
		return nil, fmt.Errorf("%w: bucket is required", bucket.ErrInvalidConfig)
	case cfg.TenancyID == "":
		return nil, fmt.Errorf("%w: tenancy id is required", bucket.ErrInvalidConfig)
	case cfg.UserID == "":
		return nil, fmt.Errorf("%w: user id is required", bucket.ErrInvalidConfig)
	case cfg.Region == "":
		return nil, fmt.Errorf("%w: region is required", bucket.ErrInvalidConfig)
	case cfg.Fingerprint == "":
		return nil, fmt.Errorf("%w: fingerprint is required", bucket.ErrInvalidConfig)
	case cfg.PrivateKey == "":
		return nil, fmt.Errorf("%w: private key is required", bucket.ErrInvalidConfig)
	}

	var passphrase *string
	if cfg.Passphrase != "" {
		passphrase = &cfg.Passphrase
	}
	provider := common.NewRawConfigurationProvider(
		cfg.TenancyID, cfg.UserID, cfg.Region, cfg.Fingerprint, cfg.PrivateKey, passphrase,
	)

	client, err := objectstorage.NewObjectStorageClientWithConfigurationProvider(provider)
	if err != nil {
		return nil, fmt.Errorf("oci bucket: client init failed: %w", err)
	}
	client.SetRegion(cfg.Region)
	if cfg.Endpoint != "" {
		client.Host = cfg.Endpoint
	}

	s := &Store{client: client, bucket: cfg.Bucket}
	if cfg.Namespace != "" {
		// Pre-seed the namespace so the first call skips the lookup.
		s.nsOnce.Do(func() { s.namespace = cfg.Namespace })
	}
	return s, nil
}

// Put uploads an object, overwriting any existing object with the same key.
func (s *Store) Put(ctx context.Context, in bucket.PutInput) error {
	if in.Key == "" {
		return fmt.Errorf("%w: key is required", bucket.ErrInvalidConfig)
	}
	if in.Body == nil {
		return fmt.Errorf("%w: body is required", bucket.ErrInvalidConfig)
	}
	ns, err := s.resolveNamespace(ctx)
	if err != nil {
		return err
	}

	req := objectstorage.PutObjectRequest{
		NamespaceName: &ns,
		BucketName:    &s.bucket,
		ObjectName:    &in.Key,
		PutObjectBody: io.NopCloser(in.Body),
	}
	if in.Size >= 0 {
		req.ContentLength = &in.Size
	}
	if in.ContentType != "" {
		req.ContentType = &in.ContentType
	}

	if _, err := s.client.PutObject(ctx, req); err != nil {
		return mapErr(fmt.Errorf("oci bucket: put %q: %w", in.Key, err))
	}
	return nil
}

// Get downloads an object. The caller must close the returned reader.
func (s *Store) Get(ctx context.Context, key string) (bucket.Object, io.ReadCloser, error) {
	ns, err := s.resolveNamespace(ctx)
	if err != nil {
		return bucket.Object{}, nil, err
	}

	resp, err := s.client.GetObject(ctx, objectstorage.GetObjectRequest{
		NamespaceName: &ns,
		BucketName:    &s.bucket,
		ObjectName:    &key,
	})
	if err != nil {
		return bucket.Object{}, nil, mapErr(fmt.Errorf("oci bucket: get %q: %w", key, err))
	}

	obj := bucket.Object{Key: key}
	if resp.ContentLength != nil {
		obj.Size = *resp.ContentLength
	}
	if resp.ContentType != nil {
		obj.ContentType = *resp.ContentType
	}
	return obj, resp.Content, nil
}

// List returns every object whose key starts with prefix, following the
// backend's pagination until the listing is exhausted.
func (s *Store) List(ctx context.Context, prefix string) ([]bucket.Object, error) {
	ns, err := s.resolveNamespace(ctx)
	if err != nil {
		return nil, err
	}

	objects := make([]bucket.Object, 0)
	var start *string
	for {
		req := objectstorage.ListObjectsRequest{
			NamespaceName: &ns,
			BucketName:    &s.bucket,
			Start:         start,
			Fields:        common.String("name,size,timeCreated"),
		}
		if prefix != "" {
			req.Prefix = &prefix
		}

		resp, err := s.client.ListObjects(ctx, req)
		if err != nil {
			return nil, mapErr(fmt.Errorf("oci bucket: list %q: %w", prefix, err))
		}

		for _, o := range resp.Objects {
			obj := bucket.Object{}
			if o.Name != nil {
				obj.Key = *o.Name
			}
			if o.Size != nil {
				obj.Size = *o.Size
			}
			if o.TimeCreated != nil {
				obj.LastModified = o.TimeCreated.Time
			}
			objects = append(objects, obj)
		}

		if resp.NextStartWith == nil || *resp.NextStartWith == "" {
			break
		}
		start = resp.NextStartWith
	}
	return objects, nil
}

// Delete removes an object. It is idempotent: a missing key is treated as a
// successful delete, matching the abstraction's contract.
func (s *Store) Delete(ctx context.Context, key string) error {
	ns, err := s.resolveNamespace(ctx)
	if err != nil {
		return err
	}

	if _, err := s.client.DeleteObject(ctx, objectstorage.DeleteObjectRequest{
		NamespaceName: &ns,
		BucketName:    &s.bucket,
		ObjectName:    &key,
	}); err != nil {
		mapped := mapErr(fmt.Errorf("oci bucket: delete %q: %w", key, err))
		if errors.Is(mapped, bucket.ErrNotFound) {
			return nil
		}
		return mapped
	}
	return nil
}

// resolveNamespace fetches the tenancy's Object Storage namespace once and
// caches the result (or the failure) for subsequent calls.
func (s *Store) resolveNamespace(ctx context.Context) (string, error) {
	s.nsOnce.Do(func() {
		resp, err := s.client.GetNamespace(ctx, objectstorage.GetNamespaceRequest{})
		if err != nil {
			s.nsErr = fmt.Errorf("oci bucket: resolve namespace: %w", err)
			return
		}
		if resp.Value != nil {
			s.namespace = *resp.Value
		}
	})
	return s.namespace, s.nsErr
}

// mapErr translates OCI 404 service errors into bucket.ErrNotFound while
// preserving the original message via wrapping.
func mapErr(err error) error {
	var svc common.ServiceError
	if errors.As(err, &svc) && svc.GetHTTPStatusCode() == http.StatusNotFound {
		return fmt.Errorf("%w: %v", bucket.ErrNotFound, err)
	}
	return err
}
