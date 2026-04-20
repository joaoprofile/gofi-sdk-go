package netx

import (
	"bytes"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go/aws/credentials"
	v4 "github.com/aws/aws-sdk-go/aws/signer/v4"
)

const (
	SERVICE_NAME = "api"
)

type AwsSigner struct {
	aws4Signer *v4.Signer
	creds      *credentials.Credentials
	hostAuth   *HostAuthentication
}

func NewAwsSigner(hostAuth *HostAuthentication) *AwsSigner {
	creds := credentials.NewStaticCredentials(hostAuth.IAMKeyId, hostAuth.IAMSecretKey, "")

	signer := v4.NewSigner(creds)
	return &AwsSigner{
		aws4Signer: signer,
		creds:      creds,
		hostAuth:   hostAuth,
	}
}

func (s *AwsSigner) Sign(originalRequest *http.Request, body []byte) (*http.Request, error) {
	originalRequest.Body = io.NopCloser(bytes.NewReader(body))
	originalRequest.ContentLength = int64(len(body))

	_, err := s.aws4Signer.Sign(originalRequest, bytes.NewReader(body), SERVICE_NAME, s.hostAuth.Region, time.Now())
	if err != nil {
		return nil, err
	}

	return originalRequest, nil
}
