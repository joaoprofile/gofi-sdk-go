package core

import "errors"

var (
	// ErrBrokerRequired is returned when New() is called without a Broker provider.
	ErrBrokerRequired = errors.New("msq: broker provider is required")

	// ErrTopicRequired is returned when a ConsumeConfig has an empty Topic.
	ErrTopicRequired = errors.New("msq: ConsumeConfig.Topic must not be empty")

	// ErrConsumerFailed is returned when a consumer exits with an unrecoverable error.
	ErrConsumerFailed = errors.New("msq: consumer failed")
)

var (
	// ErrManagerClosed is returned by Start and Healthy after Close.
	ErrManagerClosed = errors.New("msq: consumer manager closed")

	// ErrConsumerDown is reported by Healthy for a consumer that stays down.
	ErrConsumerDown = errors.New("msq: consumer down")
)
