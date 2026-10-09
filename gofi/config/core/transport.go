package core

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"slices"
	"strings"

	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
)

// Resource names accepted by GOFI_ALLOW_INSECURE_TRANSPORT.
const (
	ResourceDatabase  = "database"
	ResourceCache     = "cache"
	ResourceMessaging = "messaging"
	ResourceOTLP      = "otlp"
	ResourceBucket    = "bucket"
	// ResourceTLS is TLS_INSECURE_SKIP_VERIFY on http.DefaultTransport.
	ResourceTLS = "tls"
	// ResourceHTTP is the HTTP server served without TLS.
	ResourceHTTP = "http"
	// ResourceGRPC is the gRPC server served without TLS.
	ResourceGRPC = "grpc"
	// AllowAll in GOFI_ALLOW_INSECURE_TRANSPORT allows every resource.
	AllowAll = "all"
)

// ErrInsecureTransport is wrapped by every refusal of CheckTransport.
var ErrInsecureTransport = errors.New("insecure transport refused in production")

// InsecureTransport is a connection that would run in plaintext or without
// verifying the server certificate.
type InsecureTransport struct {
	Resource string // name accepted by GOFI_ALLOW_INSECURE_TRANSPORT
	Setting  string // environment variable that fixes it
	Detail   string // what is insecure
}

// InsecureTransports reports the process-wide settings that disable TLS
// verification or send data in plaintext: TLS_INSECURE_SKIP_VERIFY and an
// S3-compatible bucket endpoint over http. Components report their own.
func InsecureTransports(env *environment.Environment) []InsecureTransport {
	var out []InsecureTransport
	if env.TLSInsecureSkipVerify {
		out = append(out, InsecureTransport{ResourceTLS, "TLS_INSECURE_SKIP_VERIFY", "certificate verification is disabled"})
	}
	if bucketOverHTTP(env) {
		out = append(out, InsecureTransport{ResourceBucket, "BUCKET_ENDPOINT / BUCKET_S3_USE_SSL", "endpoint " + hostOf(env.BucketEndpoint) + " uses http"})
	}
	return out
}

// InsecureCache reports the CACHE_* Redis connection in plaintext to a
// non-loopback host. Components that open it (cache, session, iam) return it.
func InsecureCache(env *environment.Environment) []InsecureTransport {
	if env.CacheUseTLS || IsLoopback(env.CacheURI) {
		return nil
	}
	return []InsecureTransport{{ResourceCache, "CACHE_USE_TLS", "redis " + hostOf(env.CacheURI) + " without TLS"}}
}

// InsecureHTTP reports an HTTP server without TLS. Most deployments terminate
// TLS at an ingress, load balancer or mesh sidecar and reach the pod in
// plaintext inside the cluster, so refusing it would break them: in prod and
// stage it is only logged as a warning, unless HTTP_REQUIRE_TLS=true, which
// reports it for CheckTransport to refuse.
func InsecureHTTP(env *environment.Environment, tlsEnabled bool) []InsecureTransport {
	if tlsEnabled {
		return nil
	}
	it := InsecureTransport{ResourceHTTP, "HTTP_TLS_CERT_FILE / HTTP_TLS_KEY_FILE", "HTTP server without TLS"}
	if env.HTTPRequireTLS {
		return []InsecureTransport{it}
	}
	if isGuarded(env) {
		logging.Warn("HTTP server without TLS: terminate TLS before it (ingress, mesh) or set HTTP_TLS_CERT_FILE / HTTP_TLS_KEY_FILE; HTTP_REQUIRE_TLS=true refuses it",
			slog.String("resource", it.Resource))
	}
	return nil
}

// InsecureGRPC reports a gRPC server without TLS, with the same policy as
// InsecureHTTP: a warning in prod and stage, a refusal with GRPC_REQUIRE_TLS.
// Internal gRPC is often plaintext inside a private network or behind a mesh
// that encrypts the hop.
func InsecureGRPC(env *environment.Environment, tlsEnabled bool) []InsecureTransport {
	if tlsEnabled {
		return nil
	}
	it := InsecureTransport{ResourceGRPC, "GRPC_TLS_CERT_FILE / GRPC_TLS_KEY_FILE", "gRPC server without TLS"}
	if env.GRPCRequireTLS {
		return []InsecureTransport{it}
	}
	if isGuarded(env) {
		logging.Warn("gRPC server without TLS: keep it on a private network or mesh, or set GRPC_TLS_CERT_FILE / GRPC_TLS_KEY_FILE; GRPC_REQUIRE_TLS=true refuses it",
			slog.String("resource", it.Resource))
	}
	return nil
}

// isGuarded reports prod and stage, the environments CheckTransport checks.
func isGuarded(env *environment.Environment) bool {
	t := env.GetEnvironmentType()
	return t == environment.ENV_PROD || t == environment.ENV_STAGE
}

// hostOf drops the credentials, path and query a URL may carry.
func hostOf(uri string) string {
	if u, err := url.Parse(uri); err == nil && u.Host != "" {
		return u.Host
	}
	return uri
}

// bucketOverHTTP reports an S3-compatible endpoint reached over plain http.
func bucketOverHTTP(env *environment.Environment) bool {
	if env.BucketProvider != "s3" && env.BucketProvider != "minio" {
		return false
	}
	ep := env.BucketEndpoint
	if ep == "" || IsLoopback(ep) {
		return false
	}
	if scheme, _, ok := strings.Cut(ep, "://"); ok {
		return strings.EqualFold(scheme, "http")
	}
	return !env.BucketS3UseSSL
}

// IsLoopback reports whether addr (host, host:port or URL) is empty, localhost
// or a loopback address: plaintext to it never leaves the machine.
func IsLoopback(addr string) bool {
	host := addr
	if u, err := url.Parse(addr); err == nil && u.Host != "" {
		host = u.Host
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// CheckTransport refuses the insecure transports found in prod and stage,
// unless GOFI_ALLOW_INSECURE_TRANSPORT names their resource (or "all"), in
// which case each one is logged as a warning. Other environments are not checked.
func CheckTransport(env *environment.Environment, found []InsecureTransport) error {
	if !isGuarded(env) {
		return nil
	}
	allowed := allowedResources(env.AllowInsecureTransport)
	var errs []error
	var seen []InsecureTransport
	for _, it := range found {
		if slices.Contains(seen, it) {
			continue
		}
		seen = append(seen, it)
		if slices.Contains(allowed, it.Resource) || slices.Contains(allowed, AllowAll) {
			logging.Warn("insecure transport allowed by GOFI_ALLOW_INSECURE_TRANSPORT",
				slog.String("resource", it.Resource), slog.String("setting", it.Setting), slog.String("detail", it.Detail))
			continue
		}
		errs = append(errs, fmt.Errorf("%w: %s: %s (fix %s, or allow it with GOFI_ALLOW_INSECURE_TRANSPORT=%s)",
			ErrInsecureTransport, it.Resource, it.Detail, it.Setting, it.Resource))
	}
	return errors.Join(errs...)
}

// allowedResources parses the GOFI_ALLOW_INSECURE_TRANSPORT CSV.
func allowedResources(csv string) []string {
	var out []string
	for p := range strings.SplitSeq(csv, ",") {
		if v := strings.ToLower(strings.TrimSpace(p)); v != "" {
			out = append(out, v)
		}
	}
	return out
}
