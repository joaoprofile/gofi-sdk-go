package kafka

import (
	"crypto/sha256"
	"crypto/sha512"
	"hash"

	"github.com/xdg-go/scram"
)

// SCRAM client adapter required by Sarama to authenticate with
// SASL/SCRAM-SHA-256 or SASL/SCRAM-SHA-512 brokers (e.g. OCI managed Kafka).

var (
	sha256GeneratorFcn scram.HashGeneratorFcn = func() hash.Hash { return sha256.New() }
	sha512GeneratorFcn scram.HashGeneratorFcn = func() hash.Hash { return sha512.New() }
)

type scramClient struct {
	*scram.Client
	*scram.ClientConversation
	scram.HashGeneratorFcn
}

func (c *scramClient) Begin(userName, password, authzID string) error {
	client, err := c.HashGeneratorFcn.NewClient(userName, password, authzID)
	if err != nil {
		return err
	}
	c.Client = client
	c.ClientConversation = c.Client.NewConversation()
	return nil
}

func (c *scramClient) Step(challenge string) (string, error) {
	return c.ClientConversation.Step(challenge)
}

func (c *scramClient) Done() bool {
	return c.ClientConversation.Done()
}
