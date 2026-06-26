// Package minio implements bucket.Store against a MinIO server (and any other
// S3-compatible service) using the AWS S3 client with path-style addressing.
//
// Build a Store with New. To select the backend from configuration at runtime,
// use the base/bucket/bucketenv wiring instead of importing this package
// directly.
package minio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/joaoprofile/gofi/base/bucket"
)

// defaultRegion is sent to MinIO when none is configured. MinIO ignores it but
// the S3 signing process requires a non-empty region.
const defaultRegion = "us-east-1"

// Config holds the settings required to reach a single bucket on a MinIO /
// S3-compatible server.
type Config struct {
	// Bucket is the target bucket name. Required.
	Bucket string
	// Endpoint is the server address, e.g. "localhost:9000" or
	// "minio.internal:9000". Required.
	Endpoint string
	// AccessKey and SecretKey are the static credentials. Both required.
	AccessKey string
	SecretKey string
	// Region is sent during request signing. Defaults to "us-east-1" when empty.
	Region string
	// UseSSL selects HTTPS when true, HTTP otherwise.
	UseSSL bool
}

// Store is the MinIO/S3 implementation of bucket.Store, scoped to one bucket.
type Store struct {
	client *s3.S3
	bucket string
}

// compile-time guarantee that *Store satisfies the abstraction.
var _ bucket.Store = (*Store)(nil)

// New builds a MinIO-backed Store from cfg. It validates the configuration and
// constructs the S3 client but performs no network I/O; the server is contacted
// only when a Store method is called.
func New(cfg Config) (*Store, error) {
	switch {
	case cfg.Bucket == "":
		return nil, fmt.Errorf("%w: bucket is required", bucket.ErrInvalidConfig)
	case cfg.Endpoint == "":
		return nil, fmt.Errorf("%w: endpoint is required", bucket.ErrInvalidConfig)
	case cfg.AccessKey == "":
		return nil, fmt.Errorf("%w: access key is required", bucket.ErrInvalidConfig)
	case cfg.SecretKey == "":
		return nil, fmt.Errorf("%w: secret key is required", bucket.ErrInvalidConfig)
	}

	region := cfg.Region
	if region == "" {
		region = defaultRegion
	}

	sess, err := session.NewSession(&aws.Config{
		Endpoint:         aws.String(cfg.Endpoint),
		Region:           aws.String(region),
		DisableSSL:       aws.Bool(!cfg.UseSSL),
		S3ForcePathStyle: aws.Bool(true), // MinIO requires path-style addressing.
		Credentials:      credentials.NewStaticCredentials(cfg.AccessKey, cfg.SecretKey, ""),
	})
	if err != nil {
		return nil, fmt.Errorf("minio bucket: session init failed: %w", err)
	}

	return &Store{client: s3.New(sess), bucket: cfg.Bucket}, nil
}

// Put uploads an object, overwriting any existing object with the same key.
//
// The S3 client requires a seekable body; when in.Body is not an io.ReadSeeker
// it is fully buffered in memory before upload.
func (s *Store) Put(ctx context.Context, in bucket.PutInput) error {
	if in.Key == "" {
		return fmt.Errorf("%w: key is required", bucket.ErrInvalidConfig)
	}
	if in.Body == nil {
		return fmt.Errorf("%w: body is required", bucket.ErrInvalidConfig)
	}
	body, err := toReadSeeker(in.Body)
	if err != nil {
		return fmt.Errorf("minio bucket: put %q: %w", in.Key, err)
	}

	input := &s3.PutObjectInput{
		Bucket: &s.bucket,
		Key:    &in.Key,
		Body:   body,
	}
	if in.ContentType != "" {
		input.ContentType = &in.ContentType
	}

	if _, err := s.client.PutObjectWithContext(ctx, input); err != nil {
		return mapErr(fmt.Errorf("minio bucket: put %q: %w", in.Key, err))
	}
	return nil
}

// Get downloads an object. The caller must close the returned reader.
func (s *Store) Get(ctx context.Context, key string) (bucket.Object, io.ReadCloser, error) {
	out, err := s.client.GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	})
	if err != nil {
		return bucket.Object{}, nil, mapErr(fmt.Errorf("minio bucket: get %q: %w", key, err))
	}

	obj := bucket.Object{Key: key}
	if out.ContentLength != nil {
		obj.Size = *out.ContentLength
	}
	if out.ContentType != nil {
		obj.ContentType = *out.ContentType
	}
	return obj, out.Body, nil
}

// List returns every object whose key starts with prefix, following the
// continuation tokens until the listing is exhausted.
func (s *Store) List(ctx context.Context, prefix string) ([]bucket.Object, error) {
	objects := make([]bucket.Object, 0)
	var token *string
	for {
		input := &s3.ListObjectsV2Input{
			Bucket:            &s.bucket,
			ContinuationToken: token,
		}
		if prefix != "" {
			input.Prefix = &prefix
		}

		out, err := s.client.ListObjectsV2WithContext(ctx, input)
		if err != nil {
			return nil, mapErr(fmt.Errorf("minio bucket: list %q: %w", prefix, err))
		}

		for _, o := range out.Contents {
			obj := bucket.Object{}
			if o.Key != nil {
				obj.Key = *o.Key
			}
			if o.Size != nil {
				obj.Size = *o.Size
			}
			if o.LastModified != nil {
				obj.LastModified = *o.LastModified
			}
			objects = append(objects, obj)
		}

		if out.IsTruncated == nil || !*out.IsTruncated {
			break
		}
		token = out.NextContinuationToken
	}
	return objects, nil
}

// Delete removes an object. S3 delete is idempotent, so a missing key is not an
// error — matching the abstraction's contract.
func (s *Store) Delete(ctx context.Context, key string) error {
	if _, err := s.client.DeleteObjectWithContext(ctx, &s3.DeleteObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	}); err != nil {
		return mapErr(fmt.Errorf("minio bucket: delete %q: %w", key, err))
	}
	return nil
}

// toReadSeeker returns r when it already supports seeking, otherwise it buffers
// the whole stream in memory so the S3 client can compute the body length and
// retry the upload.
func toReadSeeker(r io.Reader) (io.ReadSeeker, error) {
	if rs, ok := r.(io.ReadSeeker); ok {
		return rs, nil
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("buffering body: %w", err)
	}
	return bytes.NewReader(data), nil
}

// mapErr translates S3 "not found" responses into bucket.ErrNotFound while
// preserving the original message via wrapping.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	var rf awserr.RequestFailure
	if errors.As(err, &rf) && rf.StatusCode() == http.StatusNotFound {
		return fmt.Errorf("%w: %v", bucket.ErrNotFound, err)
	}
	var ae awserr.Error
	if errors.As(err, &ae) {
		switch ae.Code() {
		case s3.ErrCodeNoSuchKey, s3.ErrCodeNoSuchBucket, "NotFound":
			return fmt.Errorf("%w: %v", bucket.ErrNotFound, err)
		}
	}
	return err
}
