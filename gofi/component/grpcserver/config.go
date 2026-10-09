package grpcserver

import (
	"crypto/tls"
	"errors"
	"fmt"
	"strings"

	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	"github.com/joaoprofile/gofi-sdk-go/netx"
)

// clientAuthModes maps GRPC_TLS_CLIENT_AUTH to tls.ClientAuthType.
var clientAuthModes = map[string]tls.ClientAuthType{
	"none":               tls.NoClientCert,
	"request":            tls.RequestClientCert,
	"require":            tls.RequireAnyClientCert,
	"verify_if_given":    tls.VerifyClientCertIfGiven,
	"require_and_verify": tls.RequireAndVerifyClientCert,
}

// ConfigFromEnv builds the server TLS from the environment, nil when no
// GRPC_TLS_* variable is set:
//
//   - GRPC_TLS_CERT_FILE / GRPC_TLS_KEY_FILE → certificate (both or neither,
//     reloaded on change)
//   - GRPC_TLS_CLIENT_CA_FILE                → client CAs (mTLS)
//   - GRPC_TLS_CLIENT_AUTH                   → none | request | require |
//     verify_if_given | require_and_verify (default require_and_verify with
//     a client CA, none without)
//
// Invalid values are returned as errors, never panics.
func ConfigFromEnv(env *environment.Environment) (*netx.TLSConfig, error) {
	cert := strings.TrimSpace(env.GRPCTLSCertFile)
	key := strings.TrimSpace(env.GRPCTLSKeyFile)
	ca := strings.TrimSpace(env.GRPCTLSClientCAFile)
	mode := strings.ToLower(strings.TrimSpace(env.GRPCTLSClientAuth))

	if cert == "" && key == "" {
		if ca != "" || mode != "" {
			return nil, errors.New("GRPC_TLS_CLIENT_CA_FILE / GRPC_TLS_CLIENT_AUTH need GRPC_TLS_CERT_FILE and GRPC_TLS_KEY_FILE")
		}
		return nil, nil
	}
	if cert == "" || key == "" {
		return nil, errors.New("GRPC_TLS_CERT_FILE and GRPC_TLS_KEY_FILE must be set together")
	}
	auth, err := clientAuth(mode, ca != "")
	if err != nil {
		return nil, err
	}
	return &netx.TLSConfig{CertFile: cert, KeyFile: key, ClientCAFile: ca, ClientAuth: auth}, nil
}

// clientAuth parses GRPC_TLS_CLIENT_AUTH, rejecting modes that contradict
// the presence of a client CA.
func clientAuth(mode string, hasCA bool) (tls.ClientAuthType, error) {
	if mode == "" {
		return tls.NoClientCert, nil // netx requires and verifies when a CA is set
	}
	auth, ok := clientAuthModes[mode]
	switch {
	case !ok:
		return 0, fmt.Errorf("GRPC_TLS_CLIENT_AUTH=%q: want none, request, require, verify_if_given or require_and_verify", mode)
	case auth == tls.NoClientCert && hasCA:
		return 0, errors.New("GRPC_TLS_CLIENT_AUTH=none contradicts GRPC_TLS_CLIENT_CA_FILE")
	case (auth == tls.VerifyClientCertIfGiven || auth == tls.RequireAndVerifyClientCert) && !hasCA:
		return 0, fmt.Errorf("GRPC_TLS_CLIENT_AUTH=%s needs GRPC_TLS_CLIENT_CA_FILE", mode)
	}
	return auth, nil
}
