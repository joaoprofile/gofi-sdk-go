// Package s3 implements bucket.Store for Amazon S3 and S3-compatible services
// (MinIO, Cloudflare R2, ...). Importing it registers the "s3" and "minio"
// providers for bucket.Open.
package s3

//lint:file-ignore SA1019 manager.Uploader stays until the transfermanager migration; it is not a security issue.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/gofi-labs/gofi-sdk-go/base/bucket"
	cloudaws "github.com/gofi-labs/gofi-sdk-go/base/cloud/aws"
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
	_ bucket.Store  = (*Store)(nil)
	_ bucket.Walker = (*Store)(nil)
)

func init() {
	open := func(ctx context.Context, bc bucket.Config) (bucket.Store, error) {
		c := bc.S3Credentials
		return New(ctx, Config{
			Bucket:        bc.Name,
			PresignMaxTTL: bc.PresignMaxTTL,
			AWS: cloudaws.Config{
				Region:          bc.Region,
				Endpoint:        withScheme(bc.Endpoint, c.UseSSL),
				AccessKeyID:     c.AccessKey,
				SecretAccessKey: c.SecretKey,
			},
		})
	}
	bucket.Register(bucket.ProviderS3, open)
	bucket.Register(bucket.ProviderMinIO, open)
}

// New builds a Store; credentials resolve through base/cloud/aws.
func New(ctx context.Context, cfg Config) (*Store, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("%w: bucket is required", bucket.ErrInvalidConfig)
	}
	awsCfg, err := cloudaws.Load(ctx, cfg.AWS)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", bucket.ErrInvalidConfig, err)
	}
	if awsCfg.Region == "" {
		awsCfg.Region = "us-east-1" // S3-compatible services usually ignore it but SigV4 needs one
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = cfg.PathStyle || cfg.AWS.Endpoint != ""
	})
	return &Store{
		client:     client,
		presign:    s3.NewPresignClient(client),
		upload:     manager.NewUploader(client),
		bucket:     cfg.Bucket,
		presignMax: bucket.PresignLimit(cfg.PresignMaxTTL),
	}, nil
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
				obj := bucket.Object{Key: awssdk.ToString(o.Key), Size: awssdk.ToInt64(o.Size)}
				if o.LastModified != nil {
					obj.LastModified = *o.LastModified
				}
				if !yield(obj, nil) {
					return
				}
			}
		}
	}
}

func (s *Store) List(ctx context.Context, prefix string) ([]bucket.Object, error) {
	return bucket.Collect(s.All(ctx, prefix))
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

// mapErr turns missing keys/buckets into bucket.ErrNotFound, keeping the cause.
func mapErr(err error) error {
	if re, ok := errors.AsType[*awshttp.ResponseError](err); ok && re.HTTPStatusCode() == http.StatusNotFound {
		return fmt.Errorf("%w: %w", bucket.ErrNotFound, err)
	}
	if ae, ok := errors.AsType[smithy.APIError](err); ok {
		switch ae.ErrorCode() {
		case "NoSuchKey", "NoSuchBucket", "NotFound":
			return fmt.Errorf("%w: %w", bucket.ErrNotFound, err)
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
