package core

import (
	"crypto/tls"
	"log/slog"
	"net/http"

	"github.com/joaoprofile/gofi-sdk-go/base/environment"
)

// ApplyTLS applies TLS_INSECURE_SKIP_VERIFY to http.DefaultTransport; it is a no-op by default.
func ApplyTLS(env *environment.Environment) {
	if !env.TLSInsecureSkipVerify {
		return
	}
	t, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return
	}
	cfg := &tls.Config{}
	if t.TLSClientConfig != nil {
		cfg = t.TLSClientConfig.Clone()
	}
	cfg.InsecureSkipVerify = true //nolint:gosec // explicit opt-in via TLS_INSECURE_SKIP_VERIFY
	t.TLSClientConfig = cfg
	slog.Warn("TLS certificate verification is disabled for http.DefaultTransport (TLS_INSECURE_SKIP_VERIFY=true)")
}
