package netx

import (
	"net/http"
)

type Signature interface {
	Sign(originalRequest *http.Request, body []byte) (*http.Request, error)
}

type HostAuthentication struct {
	IAMKeyId     string
	IAMSecretKey string
	Region       string
}

func NewHostAuthentication(IAMKeyId, IAMSecretKey, region string) *HostAuthentication {
	return &HostAuthentication{
		IAMKeyId:     IAMKeyId,
		IAMSecretKey: IAMSecretKey,
		Region:       region,
	}
}
