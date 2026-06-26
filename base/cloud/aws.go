package cloud

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
)

func init() {
	RegisterProvider(ProviderAWS, func(cfg Config) Provider {
		return NewAWS(cfg)
	})
}

// AWS implements Provider for Amazon Web Services.
type AWS struct {
	cfg     Config
	session *session.Session
}

func NewAWS(cfg Config) *AWS {
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
