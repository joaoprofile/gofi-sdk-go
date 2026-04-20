package cloud

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/joaoprofile/gofi/base/environment"
)

func init() {
	RegisterProvider(environment.CLOUD_AWS, func(cfg environment.CloudConfig) Provider {
		return NewAWS(cfg)
	})
}

// AWS implements Provider for Amazon Web Services.
type AWS struct {
	cfg     environment.CloudConfig
	session *session.Session
}

func NewAWS(cfg environment.CloudConfig) *AWS {
	return &AWS{cfg: cfg}
}

func (a *AWS) Bootstrap() error {
	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(a.cfg.Region),
		Endpoint:    aws.String(a.cfg.Host),
		DisableSSL:  aws.Bool(a.cfg.DisableSSL),
		Credentials: credentials.NewStaticCredentials(a.cfg.Token, a.cfg.Secret, ""),
	})
	if err != nil {
		return fmt.Errorf("AWS session initialisation failed: %w", err)
	}
	a.session = sess
	return nil
}

func (a *AWS) GetSession() any {
	return a.session
}
