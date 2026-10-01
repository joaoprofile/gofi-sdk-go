// Package s3 implements bucket.Store for Amazon S3 and S3-compatible services
// (MinIO, Cloudflare R2, ...). Importing it registers the "s3" and "minio"
// providers for bucket.Open and bucket.OpenManager.
package s3

//lint:file-ignore SA1019 manager.Uploader stays until the transfermanager migration; it is not a security issue.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"slices"
	"strings"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/joaoprofile/gofi-sdk-go/base/bucket"
	cloudaws "github.com/joaoprofile/gofi-sdk-go/base/cloud/aws"
)

// Config configures the store. With an empty AWS config the default
// credential chain and region are used.
type Config struct {
	Bucket string
	AWS    cloudaws.Config
	// PathStyle forces path-style addressing; it is implied by a custom
	// AWS.Endpoint, which MinIO and most S3-compatible services require.
	PathStyle bool
	// PresignMaxTTL lowers the longest presigned URL validity; see
	// bucket.PresignLimit.
	PresignMaxTTL time.Duration
}

// Store is an S3-backed bucket.Store.
type Store struct {
	client  *s3.Client
	presign *s3.PresignClient
	upload  *manager.Uploader
	bucket  string
	// presignMax caps PresignGet ttl.
	presignMax time.Duration
}

var (
	_ bucket.Store        = (*Store)(nil)
	_ bucket.Walker       = (*Store)(nil)
	_ bucket.DirLister    = (*Store)(nil)
	_ bucket.BatchDeleter = (*Store)(nil)
	_ bucket.Statter      = (*Store)(nil)
)

func init() {
	open := func(ctx context.Context, bc bucket.Config) (bucket.Store, error) {
		return New(ctx, configFrom(bc))
	}
	openManager := func(ctx context.Context, bc bucket.Config) (bucket.Manager, error) {
		return NewManager(ctx, configFrom(bc))
	}
	for _, p := range []bucket.Provider{bucket.ProviderS3, bucket.ProviderMinIO} {
		bucket.Register(p, open)
		bucket.RegisterManager(p, openManager)
	}
}

func configFrom(bc bucket.Config) Config {
	c := bc.S3Credentials
	return Config{
		Bucket:        bc.Name,
		PresignMaxTTL: bc.PresignMaxTTL,
		AWS: cloudaws.Config{
			Region:          bc.Region,
			Endpoint:        withScheme(bc.Endpoint, c.UseSSL),
			AccessKeyID:     c.AccessKey,
			SecretAccessKey: c.SecretKey,
		},
	}
}

// defaultRegion is used when none is configured: S3-compatible services
// usually ignore it but SigV4 needs one, and it is the S3 default.
const defaultRegion = "us-east-1"

// New builds a Store; credentials resolve through base/cloud/aws.
func New(ctx context.Context, cfg Config) (*Store, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("%w: bucket is required", bucket.ErrInvalidConfig)
	}
	awsCfg, err := load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return newStore(newClient(awsCfg, cfg, ""), cfg.Bucket, cfg.PresignMaxTTL), nil
}

func load(ctx context.Context, cfg Config) (awssdk.Config, error) {
	awsCfg, err := cloudaws.Load(ctx, cfg.AWS)
	if err != nil {
		return awssdk.Config{}, fmt.Errorf("%w: %w", bucket.ErrInvalidConfig, err)
	}
	if awsCfg.Region == "" {
		awsCfg.Region = defaultRegion
	}
	return awsCfg, nil
}

// newClient builds a client for region, or for awsCfg's region when empty.
func newClient(awsCfg awssdk.Config, cfg Config, region string) *s3.Client {
	if region != "" {
		awsCfg.Region = region
	}
	return s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = cfg.PathStyle || cfg.AWS.Endpoint != ""
	})
}

func newStore(client *s3.Client, name string, presignMax time.Duration) *Store {
	return &Store{
		client:     client,
		presign:    s3.NewPresignClient(client),
		upload:     manager.NewUploader(client),
		bucket:     name,
		presignMax: bucket.PresignLimit(presignMax),
	}
}

// Put streams the body; unknown sizes and large objects use multipart upload
// without buffering the whole object in memory.
func (s *Store) Put(ctx context.Context, in bucket.PutInput) error {
	if in.Key == "" {
		return fmt.Errorf("%w: key is required", bucket.ErrInvalidConfig)
	}
	if in.Body == nil {
		return fmt.Errorf("%w: body is required", bucket.ErrInvalidConfig)
	}
	input := &s3.PutObjectInput{Bucket: &s.bucket, Key: &in.Key, Body: in.Body}
	if in.ContentType != "" {
		input.ContentType = &in.ContentType
	}
	if in.Size >= 0 && in.Size < manager.DefaultUploadPartSize {
		input.ContentLength = awssdk.Int64(in.Size)
	}
	if _, err := s.upload.Upload(ctx, input); err != nil {
		return mapErr(fmt.Errorf("s3 bucket: put %q: %w", in.Key, err))
	}
	return nil
}

func (s *Store) Get(ctx context.Context, key string) (bucket.Object, io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		return bucket.Object{}, nil, mapErr(fmt.Errorf("s3 bucket: get %q: %w", key, err))
	}
	obj := bucket.Object{Key: key, ContentType: awssdk.ToString(out.ContentType), Size: awssdk.ToInt64(out.ContentLength)}
	if out.LastModified != nil {
		obj.LastModified = *out.LastModified
	}
	return obj, out.Body, nil
}

// Stat reads the metadata with HeadObject. A HEAD response has no body, so
// a missing bucket is reported as bucket.ErrNotFound too.
func (s *Store) Stat(ctx context.Context, key string) (bucket.Object, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		return bucket.Object{}, mapErr(fmt.Errorf("s3 bucket: stat %q: %w", key, err))
	}
	obj := bucket.Object{Key: key, ContentType: awssdk.ToString(out.ContentType), Size: awssdk.ToInt64(out.ContentLength)}
	if out.LastModified != nil {
		obj.LastModified = *out.LastModified
	}
	return obj, nil
}

// All streams the listing page by page.
func (s *Store) All(ctx context.Context, prefix string) iter.Seq2[bucket.Object, error] {
	return func(yield func(bucket.Object, error) bool) {
		input := &s3.ListObjectsV2Input{Bucket: &s.bucket}
		if prefix != "" {
			input.Prefix = &prefix
		}
		for p := s3.NewListObjectsV2Paginator(s.client, input); p.HasMorePages(); {
			page, err := p.NextPage(ctx)
			if err != nil {
				yield(bucket.Object{}, mapErr(fmt.Errorf("s3 bucket: list %q: %w", prefix, err)))
				return
			}
			for _, o := range page.Contents {
				if !yield(object(o), nil) {
					return
				}
			}
		}
	}
}

func (s *Store) List(ctx context.Context, prefix string) ([]bucket.Object, error) {
	return bucket.Collect(s.All(ctx, prefix))
}

// ListDir lists one level server-side with the "/" delimiter.
func (s *Store) ListDir(ctx context.Context, prefix string) (bucket.Dir, error) {
	d := bucket.Dir{Prefix: prefix, Folders: make([]string, 0), Objects: make([]bucket.Object, 0)}
	input := &s3.ListObjectsV2Input{Bucket: &s.bucket, Delimiter: awssdk.String("/")}
	if prefix != "" {
		input.Prefix = &prefix
	}
	for p := s3.NewListObjectsV2Paginator(s.client, input); p.HasMorePages(); {
		page, err := p.NextPage(ctx)
		if err != nil {
			return bucket.Dir{}, mapErr(fmt.Errorf("s3 bucket: list dir %q: %w", prefix, err))
		}
		for _, cp := range page.CommonPrefixes {
			d.Folders = append(d.Folders, awssdk.ToString(cp.Prefix))
		}
		for _, o := range page.Contents {
			if awssdk.ToString(o.Key) != prefix {
				d.Objects = append(d.Objects, object(o))
			}
		}
	}
	return d, nil
}

// deleteBatch is the most keys DeleteObjects accepts per request.
const deleteBatch = 1000

// DeleteMany deletes keys with DeleteObjects, up to 1000 per request. Every
// key the response reports as failed becomes an error naming it.
func (s *Store) DeleteMany(ctx context.Context, keys []string) error {
	for chunk := range slices.Chunk(keys, deleteBatch) {
		ids := make([]types.ObjectIdentifier, len(chunk))
		for i, k := range chunk {
			ids[i] = types.ObjectIdentifier{Key: awssdk.String(k)}
		}
		out, err := s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: &s.bucket,
			Delete: &types.Delete{Objects: ids, Quiet: awssdk.Bool(true)},
		})
		if err != nil {
			return mapErr(fmt.Errorf("s3 bucket: delete %d keys: %w", len(chunk), err))
		}
		errs := make([]error, 0, len(out.Errors))
		for _, e := range out.Errors {
			cause := &smithy.GenericAPIError{Code: awssdk.ToString(e.Code), Message: awssdk.ToString(e.Message)}
			errs = append(errs, mapErr(fmt.Errorf("s3 bucket: delete %q: %w", awssdk.ToString(e.Key), cause)))
		}
		if err := errors.Join(errs...); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &s.bucket, Key: &key}); err != nil {
		return mapErr(fmt.Errorf("s3 bucket: delete %q: %w", key, err))
	}
	return nil
}

func (s *Store) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if key == "" {
		return "", fmt.Errorf("%w: key is required", bucket.ErrInvalidConfig)
	}
	if err := bucket.CheckPresignTTL(ttl, s.presignMax); err != nil {
		return "", err
	}
	req, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("s3 bucket: presign %q: %w", key, err)
	}
	return req.URL, nil
}

func object(o types.Object) bucket.Object {
	obj := bucket.Object{Key: awssdk.ToString(o.Key), Size: awssdk.ToInt64(o.Size)}
	if o.LastModified != nil {
		obj.LastModified = *o.LastModified
	}
	return obj
}

// mapErr wraps err with the bucket sentinel matching the S3 error code or,
// without a known code, the HTTP status; the cause is kept. The code is
// checked first so NoSuchBucket is not taken for a plain 404.
func mapErr(err error) error {
	if ae, ok := errors.AsType[smithy.APIError](err); ok {
		var sentinel error
		switch ae.ErrorCode() {
		case "NoSuchBucket":
			sentinel = bucket.ErrBucketNotFound
		case "NoSuchKey", "NotFound":
			sentinel = bucket.ErrNotFound
		case "BucketAlreadyExists", "BucketAlreadyOwnedByYou":
			sentinel = bucket.ErrBucketExists
		case "BucketNotEmpty":
			sentinel = bucket.ErrBucketNotEmpty
		case "InvalidBucketName":
			sentinel = bucket.ErrInvalidBucketName
		case "AccessDenied", "AllAccessDisabled":
			sentinel = bucket.ErrAccessDenied
		}
		if sentinel != nil {
			return fmt.Errorf("%w: %w", sentinel, err)
		}
	}
	if re, ok := errors.AsType[*awshttp.ResponseError](err); ok {
		switch re.HTTPStatusCode() {
		case http.StatusNotFound:
			return fmt.Errorf("%w: %w", bucket.ErrNotFound, err)
		case http.StatusForbidden:
			return fmt.Errorf("%w: %w", bucket.ErrAccessDenied, err)
		}
	}
	return err
}

// withScheme keeps BUCKET_ENDPOINT values like "minio:9000" working.
func withScheme(endpoint string, useSSL bool) string {
	if endpoint == "" || strings.Contains(endpoint, "://") {
		return endpoint
	}
	if useSSL {
		return "https://" + endpoint
	}
	return "http://" + endpoint
}
