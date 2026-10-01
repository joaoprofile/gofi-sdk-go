package environment

import "testing"

func TestIsValidMessagingProvider_MatchesBrokers(t *testing.T) {
	for _, p := range []string{"kafka", "rabbitmq", "sqs", "oci", "redis", "nats"} {
		if !isValidMessagingProvider(p) {
			t.Errorf("%q must be accepted", p)
		}
	}
	for _, p := range []string{"nats-typo", "sqs_sns", "aws", "gcp"} {
		if isValidMessagingProvider(p) {
			t.Errorf("%q must be rejected", p)
		}
	}
}
