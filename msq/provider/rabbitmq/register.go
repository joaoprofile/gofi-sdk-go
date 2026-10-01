package rabbitmq

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/gofi-labs/gofi-sdk-go/msq"
	"github.com/gofi-labs/gofi-sdk-go/msq/types"
)

func init() {
	msq.Register(msq.BrokerRabbitMQ, func(_ context.Context, cfg msq.ProviderConfig) (msq.Broker, error) {
		enc, err := encoding(cfg.Encoding)
		if err != nil {
			return nil, err
		}
		rawURL, opts, err := dialConfig(cfg)
		if err != nil {
			return nil, err
		}
		conn, err := DialURL(rawURL, opts...)
		if err != nil {
			return nil, err
		}
		return New(conn, cfg.Exchange, WithEncoding(enc)), nil
	})
}

func encoding(e types.Encoding) (types.Encoding, error) {
	switch e {
	case "":
		return types.EncodingEnvelope, nil
	case types.EncodingEnvelope, types.EncodingCloudEvents:
		return e, nil
	default:
		return "", fmt.Errorf("rabbitmq: invalid encoding %q (envelope or cloudevents)", e)
	}
}

// dialConfig validates the address before any credential is involved and
// builds the URL (credentials escaped) and TLS options; TLS selects amqps.
func dialConfig(cfg msq.ProviderConfig) (string, []DialOption, error) {
	host, port, err := net.SplitHostPort(cfg.Addr)
	if err != nil || host == "" || port == "" || strings.ContainsAny(host, " /@?#%") {
		return "", nil, fmt.Errorf("rabbitmq: invalid address %q (host:port)", cfg.Addr)
	}
	u := url.URL{
		Scheme: "amqp",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   cfg.Addr,
		Path:   "/",
	}
	var opts []DialOption
	if cfg.TLSEnabled() {
		u.Scheme = "amqps"
		t, err := cfg.TLS.Config()
		if err != nil {
			return "", nil, fmt.Errorf("rabbitmq: %w", err)
		}
		opts = append(opts, WithTLSConfig(t))
	}
	return u.String(), opts, nil
}
