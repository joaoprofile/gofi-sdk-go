// Package oci implements bucket.Store on top of OCI Object Storage.
//
// Build a Store with New, or blank-import this package and call bucket.Open
// to select the backend from configuration.
package oci

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/base/bucket"
	cloudoci "github.com/joaoprofile/gofi-sdk-go/base/cloud/oci"
	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/objectstorage"
	"github.com/oracle/oci-go-sdk/v65/objectstorage/transfer"
)

// Config holds the settings required to reach a single OCI Object Storage
// bucket.
type Config struct {
	Bucket string
	// Namespace is the Object Storage namespace. When empty it is resolved
	// lazily with a GetNamespace call and cached.
	Namespace string
	// Endpoint overrides the regional Object Storage endpoint.
	Endpoint string
	// Credentials selects the principal and region (see base/cloud/oci).
	Credentials cloudoci.Config
	// PresignMaxTTL lowers the longest PAR validity; see bucket.PresignLimit.
	PresignMaxTTL time.Duration
}

// Store is the OCI Object Storage implementation of bucket.Store. It targets a
// single bucket and resolves the Object Storage namespace lazily on first use.
type Store struct {
	client  objectstorage.ObjectStorageClient
	uploads *transfer.UploadManager
	bucket  string
	// parBaseURL is the scheme+host used to turn a PAR AccessUri (a path) into an
	// absolute download URL.
	parBaseURL string
	presignMax time.Duration

	nsMu      sync.Mutex
	namespace string
}

// compile-time guarantee that *Store satisfies the abstraction.
var (
	_ bucket.Store  = (*Store)(nil)
	_ bucket.Walker = (*Store)(nil)
)

// New builds an OCI-backed Store from cfg. It validates credentials and
// constructs the SDK client but performs no network I/O; the namespace and
// bucket are contacted only when a Store method is called.
func New(cfg Config) (*Store, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("%w: bucket is required", bucket.ErrInvalidConfig)
	}
	region := cfg.Credentials.Region
	if region == "" {
		return nil, fmt.Errorf("%w: region is required", bucket.ErrInvalidConfig)
	}

	provider, err := cloudoci.ConfigurationProvider(cfg.Credentials)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", bucket.ErrInvalidConfig, err)
	}

	client, err := objectstorage.NewObjectStorageClientWithConfigurationProvider(provider)
	if err != nil {
		return nil, fmt.Errorf("oci bucket: client init failed: %w", err)
	}
	client.SetRegion(region)
	if cfg.Endpoint != "" {
		client.Host = cfg.Endpoint
	}

	base := cfg.Endpoint
	if base == "" {
		base = fmt.Sprintf("https://objectstorage.%s.oraclecloud.com", region)
	}

	s := &Store{client: client, uploads: transfer.NewUploadManager(), bucket: cfg.Bucket, parBaseURL: strings.TrimRight(base, "/"),
		presignMax: bucket.PresignLimit(cfg.PresignMaxTTL)}
	if cfg.Namespace != "" {
		// Pre-seed the namespace so the first call skips the lookup.
		s.namespace = cfg.Namespace
	}
	return s, nil
}

// newConfigurationProvider selects the OCI credential source from cfg.AuthMode.
// The SDK never auto-detects instance identity, so the principal is always named
// explicitly here; an unknown mode is a configuration error.
func init() {
	bucket.Register(bucket.ProviderOCI, func(_ context.Context, bc bucket.Config) (bucket.Store, error) {
		c := bc.OCICredentials
		return New(Config{
			Bucket:        bc.Name,
			Namespace:     c.Namespace,
			Endpoint:      bc.Endpoint,
			PresignMaxTTL: bc.PresignMaxTTL,
			Credentials: cloudoci.Config{
				AuthMode:    cloudoci.AuthMode(c.AuthMode),
				Region:      bc.Region,
				TenancyID:   c.TenancyID,
				UserID:      c.UserID,
				Fingerprint: c.FingerPrint,
				PrivateKey:  c.PrivateKey,
				Passphrase:  c.Passphrase,
			},
		})
	})
}

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

	var contentType *string
	if in.ContentType != "" {
		contentType = &in.ContentType
	}
	if in.Size < 0 {
		// PutObject needs Content-Length; unknown sizes stream as multipart
		// parts without buffering the whole object.
		_, err := s.uploads.UploadStream(ctx, transfer.UploadStreamRequest{
			UploadRequest: transfer.UploadRequest{
				NamespaceName:       &ns,
				BucketName:          &s.bucket,
				ObjectName:          &in.Key,
				ContentType:         contentType,
				ObjectStorageClient: &s.client,
			},
			StreamReader: in.Body,
		})
		if err != nil {
			return mapErr(fmt.Errorf("oci bucket: put %q: %w", in.Key, err))
		}
		return nil
	}
	req := objectstorage.PutObjectRequest{
		NamespaceName: &ns,
		BucketName:    &s.bucket,
		ObjectName:    &in.Key,
		PutObjectBody: io.NopCloser(in.Body),
		ContentLength: &in.Size,
		ContentType:   contentType,
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

// All streams the listing page by page.
func (s *Store) All(ctx context.Context, prefix string) iter.Seq2[bucket.Object, error] {
	return func(yield func(bucket.Object, error) bool) {
		ns, err := s.resolveNamespace(ctx)
		if err != nil {
			yield(bucket.Object{}, err)
			return
		}
		var start *string
		for {
			req := objectstorage.ListObjectsRequest{
				NamespaceName: &ns,
				BucketName:    &s.bucket,
				Start:         start,
				Fields:        new("name,size,timeCreated"),
			}
			if prefix != "" {
				req.Prefix = &prefix
			}
			resp, err := s.client.ListObjects(ctx, req)
			if err != nil {
				yield(bucket.Object{}, mapErr(fmt.Errorf("oci bucket: list %q: %w", prefix, err)))
				return
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
				if !yield(obj, nil) {
					return
				}
			}
			if resp.NextStartWith == nil || *resp.NextStartWith == "" {
				return
			}
			start = resp.NextStartWith
		}
	}
}

// List returns every object whose key starts with prefix.
func (s *Store) List(ctx context.Context, prefix string) ([]bucket.Object, error) {
	return bucket.Collect(s.All(ctx, prefix))
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

// PresignGet issues a read-only Pre-Authenticated Request (PAR) scoped to a
// single object and returns the absolute download URL valid for ttl. Each
// call creates a PAR that lives until it expires, so ttl is capped.
func (s *Store) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if key == "" {
		return "", fmt.Errorf("%w: key is required", bucket.ErrInvalidConfig)
	}
	if err := bucket.CheckPresignTTL(ttl, s.presignMax); err != nil {
		return "", err
	}
	ns, err := s.resolveNamespace(ctx)
	if err != nil {
		return "", err
	}

	name := "dl-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	expires := &common.SDKTime{Time: time.Now().Add(ttl)}

	resp, err := s.client.CreatePreauthenticatedRequest(ctx, objectstorage.CreatePreauthenticatedRequestRequest{
		NamespaceName: &ns,
		BucketName:    &s.bucket,
		CreatePreauthenticatedRequestDetails: objectstorage.CreatePreauthenticatedRequestDetails{
			Name:        &name,
			ObjectName:  &key,
			AccessType:  objectstorage.CreatePreauthenticatedRequestDetailsAccessTypeObjectread,
			TimeExpires: expires,
		},
	})
	if err != nil {
		return "", mapErr(fmt.Errorf("oci bucket: presign %q: %w", key, err))
	}
	if resp.AccessUri == nil {
		return "", fmt.Errorf("oci bucket: presign %q: empty access uri", key)
	}
	return s.parBaseURL + *resp.AccessUri, nil
}

// resolveNamespace fetches the tenancy's Object Storage namespace and caches it;
// failures are not cached, so the next call retries.
func (s *Store) resolveNamespace(ctx context.Context) (string, error) {
	s.nsMu.Lock()
	defer s.nsMu.Unlock()
	if s.namespace != "" {
		return s.namespace, nil
	}
	// Detached from the caller so one canceled request cannot fail the lookup for others.
	resp, err := s.client.GetNamespace(context.WithoutCancel(ctx), objectstorage.GetNamespaceRequest{})
	if err != nil {
		return "", fmt.Errorf("oci bucket: resolve namespace: %w", err)
	}
	if resp.Value != nil {
		s.namespace = *resp.Value
	}
	return s.namespace, nil
}

// mapErr translates OCI 404 service errors into bucket.ErrNotFound while
// preserving the original message via wrapping.
func mapErr(err error) error {
	var svc common.ServiceError // not an error type, so errors.AsType does not apply
	if errors.As(err, &svc) && svc.GetHTTPStatusCode() == http.StatusNotFound {
		return fmt.Errorf("%w: %w", bucket.ErrNotFound, err)
	}
	return err
}
