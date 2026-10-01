package sqs

import (
	"context"

	"github.com/gofi-labs/gofi-sdk-go/msq"
)

// Identity, region and endpoint come from the AWS default chain: AWS_REGION,
// AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY, AWS_PROFILE, AWS_ENDPOINT_URL_SQS,
// IRSA / EKS Pod Identity, ECS and EC2 roles.
func init() {
	msq.Register(msq.BrokerSQS, func(ctx context.Context, _ msq.ProviderConfig) (msq.Broker, error) {
		b, err := New(ctx, Config{})
		if err != nil {
			return nil, err
		}
		return b, nil
	})
}
