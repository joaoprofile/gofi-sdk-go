package s3

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/joaoprofile/gofi-sdk-go/base/bucket"
)

// Manager administers the S3 buckets of an account. Safe for concurrent use.
type Manager struct {
	cfg    Config
	awsCfg awssdk.Config
	client *s3.Client // in awsCfg.Region
	// resolveRegion opens each bucket in its own region. It is off with a
	// custom endpoint (MinIO, R2, ...), which serves every bucket itself.
	resolveRegion bool

	mu     sync.Mutex
	stores map[string]*Store
}

var _ bucket.Manager = (*Manager)(nil)

// listBucketsPage is the MaxBuckets of each ListBuckets request; without it
// S3 leaves BucketRegion empty.
const listBucketsPage = 1000

// NewManager builds a Manager; credentials resolve through base/cloud/aws.
// cfg.Bucket is ignored.
func NewManager(ctx context.Context, cfg Config) (*Manager, error) {
	awsCfg, err := load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &Manager{
		cfg:           cfg,
		awsCfg:        awsCfg,
		client:        newClient(awsCfg, cfg, ""),
		resolveRegion: cfg.AWS.Endpoint == "",
		stores:        make(map[string]*Store),
	}, nil
}

func (m *Manager) ListBuckets(ctx context.Context) ([]bucket.BucketInfo, error) {
	out := make([]bucket.BucketInfo, 0)
	input := &s3.ListBucketsInput{MaxBuckets: awssdk.Int32(listBucketsPage)}
	for p := s3.NewListBucketsPaginator(m.client, input); p.HasMorePages(); {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, mapErr(fmt.Errorf("s3 bucket: list buckets: %w", err))
		}
		for _, b := range page.Buckets {
			out = append(out, bucket.BucketInfo{
				Name:      awssdk.ToString(b.Name),
				Region:    awssdk.ToString(b.BucketRegion),
				CreatedAt: awssdk.ToTime(b.CreationDate),
			})
		}
	}
	return out, nil
}

// CreateBucket creates name in opts.Region, else the configured region, else
// us-east-1. A bucket the account already owns is ErrBucketExists in every
// region, although us-east-1 answers a repeated CreateBucket with success.
func (m *Manager) CreateBucket(ctx context.Context, name string, opts bucket.CreateOptions) error {
	if err := bucket.ValidateName(name); err != nil {
		return err
	}
	if _, err := m.bucketRegion(ctx, name); err == nil {
		return fmt.Errorf("%w: %q", bucket.ErrBucketExists, name)
	}
	region := cmp.Or(opts.Region, m.awsCfg.Region)
	input := &s3.CreateBucketInput{Bucket: &name}
	if region != defaultRegion { // us-east-1 rejects an explicit LocationConstraint
		input.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(region),
		}
	}
	if _, err := newClient(m.awsCfg, m.cfg, region).CreateBucket(ctx, input); err != nil {
		return mapErr(fmt.Errorf("s3 bucket: create bucket %q: %w", name, err))
	}
	return nil
}

// DeleteBucket deletes name from its own region. With Force it first deletes
// every object; a versioned bucket still holds the old versions afterwards,
// so it stays bucket.ErrBucketNotEmpty.
func (m *Manager) DeleteBucket(ctx context.Context, name string, opts bucket.DeleteOptions) error {
	s, err := m.open(ctx, name)
	if err != nil {
		return err
	}
	if opts.Force {
		if _, err := bucket.DeletePrefix(ctx, s, ""); err != nil {
			return err
		}
	}
	_, err = s.client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: &name})
	if err == nil || errors.Is(mapErr(err), bucket.ErrBucketNotFound) {
		m.mu.Lock()
		delete(m.stores, name)
		m.mu.Unlock()
	}
	if err != nil {
		return mapErr(fmt.Errorf("s3 bucket: delete bucket %q: %w", name, err))
	}
	return nil
}

// Open returns the Store of name, in the bucket's own region unless a custom
// endpoint is configured. The Store is cached per name.
func (m *Manager) Open(ctx context.Context, name string) (bucket.Store, error) {
	return m.open(ctx, name)
}

func (m *Manager) open(ctx context.Context, name string) (*Store, error) {
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", bucket.ErrInvalidBucketName)
	}
	m.mu.Lock()
	s, ok := m.stores[name]
	m.mu.Unlock()
	if ok {
		return s, nil
	}
	region, err := m.bucketRegion(ctx, name)
	if err != nil {
		return nil, err
	}
	if !m.resolveRegion {
		region = ""
	}
	s = newStore(newClient(m.awsCfg, m.cfg, region), name, m.cfg.PresignMaxTTL)
	m.mu.Lock()
	defer m.mu.Unlock()
	if cached, ok := m.stores[name]; ok {
		return cached, nil
	}
	m.stores[name] = s
	return s, nil
}

// bucketRegion checks that name exists with HeadBucket and returns its
// region: the BucketRegion of the response or, when the bucket lives in
// another region, the x-amz-bucket-region header of the redirect.
func (m *Manager) bucketRegion(ctx context.Context, name string) (string, error) {
	out, err := m.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &name})
	if err == nil {
		return cmp.Or(awssdk.ToString(out.BucketRegion), m.awsCfg.Region), nil
	}
	if re, ok := errors.AsType[*awshttp.ResponseError](err); ok {
		switch re.HTTPStatusCode() {
		case http.StatusNotFound:
			return "", fmt.Errorf("%w: %q: %w", bucket.ErrBucketNotFound, name, err)
		case http.StatusForbidden:
			return "", fmt.Errorf("%w: %q: %w", bucket.ErrAccessDenied, name, err)
		case http.StatusMovedPermanently, http.StatusBadRequest:
			if re.Response != nil {
				if r := re.Response.Header.Get("X-Amz-Bucket-Region"); r != "" {
					return r, nil
				}
			}
		}
	}
	return "", fmt.Errorf("s3 bucket: head bucket %q: %w", name, err)
}
