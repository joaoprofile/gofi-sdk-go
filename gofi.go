package gofi

import (
	"context"
	"database/sql"

	"github.com/joaoprofile/gofi/base/environment"
	"github.com/joaoprofile/gofi/base/session"
	"github.com/joaoprofile/gofi/msq"
	"github.com/joaoprofile/gofi/netx"
	"github.com/joaoprofile/gofi/obs"
	"github.com/redis/go-redis/v9"
)

// Builder defines the fluent construction API for a GOFI service.
// Each method returns Builder to allow chaining.
// Call Build() to finalize and obtain a Service.
//
// Two patterns are supported:
//
//   - Add*() convenience methods read configuration from environment variables
//     and create connections automatically (zero-config).
//
//   - With*() injection methods accept pre-built instances, giving the caller
//     full control over connection lifecycle and configuration. This pattern
//     is recommended when using sub-modules independently.
type Builder interface {
	// Convenience (env-driven)
	AddDatabase() Builder
	AddCache() Builder
	AddLockerCache() Builder
	AddSession(cfg ...*session.Config) Builder
	AddObservability() Builder
	AddMessaging(cfg ...msq.Config) Builder

	// Transport (convenience)
	NewHttpServer(port string, cfg ...*netx.WSConfig) Builder
	Handlers(handlers ...netx.RouterHandler) Builder
	Use(middleware ...netx.Middleware) Builder
	UseAuth(auth netx.Middleware) Builder

	// Injection — accept pre-built instances
	WithDatabase(db *sql.DB) Builder
	WithCache(client redis.UniversalClient) Builder
	WithMessaging(broker msq.Broker) Builder
	WithObservability(tele *obs.Telemetry) Builder
	WithHttpServer(server netx.HttpServer) Builder

	// Build finalizes construction and returns the immutable Service.
	Build() Service
}

// Service defines the runtime contract of a GOFI service.
// Obtained exclusively by calling Builder.Build().
// After Build(), Add* methods are no longer accessible.
type Service interface {
	Environment() *environment.Environment
	Database() *sql.DB
	Cache() redis.UniversalClient
	Messaging() msq.Messaging
	HttpServer() netx.HttpServer
	ListenAndServe()
	Shutdown(ctx context.Context) error
}

// New initializes the GOFI SDK for the given service name and returns a Builder.
// Side effects on startup (TLS, timezone, logging, cloud, observer) happen here.
func New(serviceName string) Builder {
	return newInstance(serviceName)
}
