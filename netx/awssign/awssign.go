// Package awssign signs httpx client requests with AWS Signature V4 (API Gateway IAM
// auth, OpenSearch, Lambda URLs, ...). Credentials resolve through base/cloud/aws,
// so IRSA / Pod Identity work without static keys.
package awssign

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	cloudaws "github.com/joaoprofile/gofi-sdk-go/base/cloud/aws"
)

// ServiceExecuteAPI is the signing name of API Gateway.
const ServiceExecuteAPI = "execute-api"

// Config selects the target service and the AWS identity.
type Config struct {
	// Service is the SigV4 signing name; defaults to ServiceExecuteAPI.
	Service string
	AWS     cloudaws.Config
}

// Signer implements httpx.Signature.
type Signer struct {
	signer  *v4.Signer
	creds   awssdk.CredentialsProvider
	region  string
	service string
}

// New resolves credentials and region for cfg.
func New(ctx context.Context, cfg Config) (*Signer, error) {
	awsCfg, err := cloudaws.Load(ctx, cfg.AWS)
	if err != nil {
		return nil, err
	}
	return NewWithConfig(awsCfg, cfg.Service)
}

// NewWithConfig builds a Signer from an existing aws.Config.
func NewWithConfig(awsCfg awssdk.Config, service string) (*Signer, error) {
	if awsCfg.Region == "" {
		return nil, errors.New("awssign: region is required")
	}
	if awsCfg.Credentials == nil {
		return nil, errors.New("awssign: credentials are required")
	}
	if service == "" {
		service = ServiceExecuteAPI
	}
	// Uncached providers (STS, IMDS, ...) would be called on every request.
	creds := awsCfg.Credentials
	if _, cached := creds.(*awssdk.CredentialsCache); !cached {
		creds = awssdk.NewCredentialsCache(creds)
	}
	return &Signer{signer: v4.NewSigner(), creds: creds, region: awsCfg.Region, service: service}, nil
}

// Sign adds the SigV4 headers, covering every header already set on req.
// body must be the exact payload sent; when nil, the payload is read from
// req.GetBody, and a body that cannot be replayed is an error rather than a
// signature over the wrong payload.
func (s *Signer) Sign(req *http.Request, body []byte) (*http.Request, error) {
	ctx := req.Context()
	if body == nil {
		var err error
		if body, err = replayBody(req); err != nil {
			return nil, err
		}
	}
	creds, err := s.creds.Retrieve(ctx)
	if err != nil {
		return nil, fmt.Errorf("awssign: retrieve credentials: %w", err)
	}
	sum := sha256.Sum256(body)
	if err := s.signer.SignHTTP(ctx, creds, req, hex.EncodeToString(sum[:]), s.service, s.region, time.Now()); err != nil {
		return nil, fmt.Errorf("awssign: %w", err)
	}
	return req, nil
}

// replayBody returns the payload of req without consuming req.Body.
func replayBody(req *http.Request) ([]byte, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}
	if req.GetBody == nil {
		return nil, errors.New("awssign: request body cannot be replayed for hashing; pass the payload bytes")
	}
	rc, err := req.GetBody()
	if err != nil {
		return nil, fmt.Errorf("awssign: read body: %w", err)
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("awssign: read body: %w", err)
	}
	return b, nil
}
